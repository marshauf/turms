package turms

import (
	"fmt"
	"golang.org/x/net/context"
	"sync"
)

const (
	sessionContextKey = "session"
	errorContextKey   = "error"
)

// A Session manages retrieval of session related values in a message context.
// Implementations of Session must be safe for concurrent use by multiple goroutines.
type Session interface {
	ID() ID
	Details() map[string]interface{}

	// NextID returns a new session scope ID.
	NextID() ID
}

type session struct {
	id      ID
	details map[string]interface{}
	counter idCounter
}

func (s *session) ID() ID {
	return s.id
}

func (s *session) NextID() ID {
	return s.counter.Next()
}

func (s *session) Details() map[string]interface{} {
	return s.details
}

// NewSessionContext returns a child context with the session value stored in it.
func NewSessionContext(ctx context.Context, session Session) context.Context {
	return context.WithValue(ctx, sessionContextKey, session)
}

// SessionFromContext extracts the session value from the context.
func SessionFromContext(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(sessionContextKey).(Session)
	return s, ok
}

// NewErrorContext returns a child context with the error value stored in it.
func NewErrorContext(ctx context.Context, err error) context.Context {
	return context.WithValue(ctx, errorContextKey, err)
}

// ErrorFromContext extracts the error value from the context.
func ErrorFromContext(ctx context.Context) (error, bool) {
	s, ok := ctx.Value(errorContextKey).(error)
	return s, ok
}

type realm struct {
	name string

	clients map[Conn]*session

	mu    sync.RWMutex
	roles map[string]*role
}

type role struct {
	features map[string]bool
}

// Realm returns a handler that handles realm specific messages.
func Realm(name string) Handler {
	return &realm{
		name:    name,
		clients: make(map[Conn]*session),
	}
}

func (r *realm) RegisterRole(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, exist := r.roles[name]
	if exist {
		return fmt.Errorf("Role %s already exist", name)
	}
	r.roles[name] = nil
	return nil
}

func (r *realm) RegisterFeatures(name string, features ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rl, exist := r.roles[name]
	if !exist {
		return fmt.Errorf("Role %s does not exist", name)
	}
	if rl == nil {
		featureMap := make(map[string]bool)
		for _, feature := range features {
			featureMap[feature] = true
		}
		r.roles[name] = &role{features: featureMap}
		return nil
	}
	for _, feature := range features {
		rl.features[feature] = true
	}
	return nil
}

func handleProtocolViolation(ctx context.Context, conn Conn) error {
	defer conn.Close()
	byeMsg := &Goodbye{GoodbyeCode, map[string]interface{}{}, URI("wamp.error.protocol_violation")}
	return conn.Send(ctx, byeMsg)
}

func (r *realm) Handle(ctx context.Context, conn Conn, msg Message) context.Context {
	r.mu.RLock()
	se, hasSession := r.clients[conn]
	r.mu.RUnlock()
	switch m := msg.(type) {
	case *Hello:
		if hasSession {
			handleProtocolViolation(ctx, conn)
			ctx, cancel := context.WithCancel(ctx)
			cancel()
			return NewErrorContext(ctx, fmt.Errorf("Protocol violation"))
		}

		se = &session{}
		se.id = NewGlobalID()

		r.mu.Lock()
		r.clients[conn] = se
		r.mu.Unlock()

		welcomeMsg := &Welcome{WelcomeCode, se.ID(), r.details()}
		err := conn.Send(ctx, welcomeMsg)
		if err != nil {
			return NewErrorContext(ctx, err)
		}
		return NewSessionContext(ctx, se)
	case *Abort:
		conn.Close()
		ctx, cancel := context.WithCancel(ctx)
		cancel()
		return ctx
	case *Goodbye:
		if m.Reason != GoodbyeAndOut {
			// Reply with goodbye if received message is not a goodbye message
			byeMsg := &Goodbye{GoodbyeCode, map[string]interface{}{}, GoodbyeAndOut}
			err := conn.Send(ctx, byeMsg)
			if err != nil {
				ctx = NewErrorContext(ctx, err)
			}
		}
		conn.Close()
		ctx, cancel := context.WithCancel(ctx)
		cancel()
		return ctx
	}

	if !hasSession {
		ctx, cancel := context.WithCancel(ctx)
		cancel()
		return ctx
	}
	return NewSessionContext(ctx, se)
}

func (r *realm) details() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return map[string]interface{}{
		"roles": r.roles,
	}
}
