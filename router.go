package turms

import (
	"golang.org/x/net/context"
	"net"
	"sync"
	"time"
)

// A Router performs message routing between components
// and may handle either or both the roles of Dealer or Broker.
type Router struct {
	ctx    context.Context
	cancel func()
	// TODO Router.Handler provides a potential data race
	Handler   Handler
	mu        sync.RWMutex
	timeout   time.Duration
	conns     []Conn
	idCounter idCounter
}

// Client creates an in-memory Client.
func (r *Router) Client() *Client {
	c1, c2 := net.Pipe()
	go r.AcceptConn(c2, &JSONCodec{})

	ctx := context.Background()
	ctxHandle, cancelHandle := context.WithCancel(ctx)

	return &Client{
		conn:         NewConn(c1, &JSONCodec{}),
		ctx:          ctx,
		ctxHandle:    ctxHandle,
		cancelHandle: cancelHandle,
		timeout:      time.Second * 60,
		req:          newWaitCondHandler(),
		sub:          newSubscriber(),
		cal:          newCallee(),
	}
}

// AcceptConn starts the routing process for the connection.
func (r *Router) AcceptConn(conn net.Conn, codec Codec) {
	r.handleConn(conn, codec)
}

func (r *Router) handleConn(conn net.Conn, codec Codec) {
	c := NewConn(conn, codec)
	r.mu.Lock()
	r.conns = append(r.conns, c)
	r.mu.Unlock()
	for {
		r.mu.RLock()
		t := r.timeout
		r.mu.RUnlock()
		msg, err := waitForMessage(r.ctx, c, t)
		if err != nil {
			switch err {
			case context.Canceled:
			case context.DeadlineExceeded:
				// TODO Tell client about the timeout
				c.Close()
			default:
				c.Close()
			}
			return
		}
		r.mu.RLock()
		tCtx, cancel := context.WithTimeout(r.ctx, r.timeout)
		ctx := NewRouterContext(tCtx, &RouterContext{counter: &r.idCounter})
		r.mu.RUnlock()
		// TODO Check r.Handler, if nil use Default
		// TODO How to handle client closing in a Handler, stopping the chain and continuing the chain
		// -> Wrap the conn in a wrapper, stop: cancel ctx (stops chain execution), continue: pass ctx
		endCtx := r.Handler.Handle(ctx, c, msg)

		// err, ok := ErrorFromContext(endCtx)
		// if ok {
		// TODO Handle errors and don't print them
		// }
		select {
		case <-tCtx.Done():
			// message tCtx timed out
		case <-endCtx.Done():
			// last handler done
		default:
			cancel()
		}

	}
}

// Close sends Goodbye messages to all clients, closes the connections and stops routing.
func (r *Router) Close() error {
	select {
	case <-r.ctx.Done():
		return r.ctx.Err()
	default:
		r.mu.RLock()
		defer r.mu.RUnlock()
		for i := range r.conns {
			// TODO Collect errors
			// TODO close in a timeout context and send GOODBYE/ABORT messages
			r.conns[i].Close()
		}
		r.cancel()
		return nil
	}
}

type RouterContext struct {
	counter *idCounter
}

func (ctx *RouterContext) NextID() ID {
	return ctx.counter.Next()
}

// NewRouterContext returns a child context with the router value stored in it.
func NewRouterContext(ctx context.Context, r *RouterContext) context.Context {
	return context.WithValue(ctx, errorContextKey, r)
}

// RouterContextFromContext extracts the router value from the context.
func RouterContextFromContext(ctx context.Context) (*RouterContext, bool) {
	s, ok := ctx.Value(errorContextKey).(*RouterContext)
	return s, ok
}

// NewRouter creates a new router ready to accept new connections.
func NewRouter() *Router {
	ctx, cancel := context.WithCancel(context.Background())
	return &Router{
		ctx:     ctx,
		cancel:  cancel,
		timeout: time.Second * 3,
	}
}

// A Handler manages messages.
type Handler interface {
	Handle(context.Context, Conn, Message) context.Context
}
