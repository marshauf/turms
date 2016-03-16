package turms

import (
	"golang.org/x/net/context"
	"sync"
	"time"
)

const (
	sessionContextKey = "session"
)

type Session struct {
	id           ID
	realm        *Realm
	details      map[string]interface{}
	sessionIDGen idCounter

	routerIDGen *idCounter
}

func (s *Session) ID() ID {
	return s.id
}

func (s *Session) Realm() *Realm {
	return s.realm
}

func (s *Session) SessionID() ID {
	return s.sessionIDGen.Next()
}

func (s *Session) RouterID() ID {
	return s.routerIDGen.Next()
}

// NewSessionContext returns a child context with the session value stored in it.
func NewSessionContext(ctx context.Context, session *Session) context.Context {
	return context.WithValue(ctx, sessionContextKey, session)
}

// SessionFromContext extracts the session value from the context.
func SessionFromContext(ctx context.Context) (*Session, bool) {
	s, ok := ctx.Value(sessionContextKey).(*Session)
	return s, ok
}

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
	se := &Session{
		routerIDGen: &r.idCounter,
	}
	r.mu.Unlock()

	for {
		r.mu.RLock()
		t := r.timeout
		ctx := r.ctx
		r.mu.RUnlock()
		msg, err := c.Read(ctx)
		if err != nil {
			switch err {
			case context.Canceled:
				tctx, cancel := context.WithTimeout(context.Background(), t)
				defer cancel()
				c.Send(tctx, &Goodbye{GoodbyeCode, nil, SystemShutdown})
				c.Close()
				return
			case context.DeadlineExceeded:
				// TODO Tell client about the timeout
				c.Close()
			default:
				c.Close()
			}
			return
		}

		ctx, cancel := context.WithTimeout(ctx, t)
		ctx = NewSessionContext(ctx, se)
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

// A Chain is a chain of Handlers. It implements a Handler.
type Chain []Handler

// Handle calls each chain element Handle function subsequently.
func (c *Chain) Handle(ctx context.Context, conn Conn, msg Message) context.Context {
	for i := range *c {
		ctx = (*c)[i].Handle(ctx, conn, msg)
	}
	return ctx
}
