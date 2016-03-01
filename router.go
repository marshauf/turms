package turms

import (
	"golang.org/x/net/context"
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
	c1, c2 := Pipe()
	go r.AcceptConn(c2)

	ctx := context.Background()
	ctxHandle, cancelHandle := context.WithCancel(ctx)

	return &Client{
		conn:         c1,
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
func (r *Router) AcceptConn(conn Conn) {
	r.handleConn(conn)
}

func (r *Router) handleConn(c Conn) {
	r.mu.Lock()
	r.conns = append(r.conns, c)
	rse := &RouterSession{
		gen: &r.idCounter,
	}
	r.mu.Unlock()
	cse := &ClientSession{}

	for {
		r.mu.RLock()
		t := r.timeout
		ctx := r.ctx
		r.mu.RUnlock()
		msg, err := waitForMessage(ctx, c, t)
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

		ctx, cancel := context.WithTimeout(ctx, t)
		ctx = NewRouterContext(ctx, rse)
		ctx = NewClientSessionContext(ctx, cse)
		// TODO Check r.Handler, if nil use Default
		// TODO How to handle client closing in a Handler, stopping the chain and continuing the chain
		// -> Wrap the conn in a wrapper, stop: cancel ctx (stops chain execution), continue: pass ctx
		endCtx := r.Handler.Handle(ctx, c, msg)

		// err, ok := ErrorFromContext(endCtx)
		// if ok {
		// TODO Handle errors and don't print them
		// }
		select {
		case <-ctx.Done():
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
