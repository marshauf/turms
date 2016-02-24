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

// Session stores the values and optional configuration for a session.
type Session struct {
	// ID is the session ID.
	ID ID
	// Details stores additional connection opening information.
	Details map[string]interface{}

	Realm URI

	// The ID generator at the session scope
	counter idCounter
	// The ID generator at the router scope
	routerIDGen *idCounter
}

// SessionID returns a new ID in the session scope.
func (s *Session) SessionID() ID {
	return s.counter.Next()
}

// RouterID returns a new ID in the router scope.
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
	name  URI
	mu    sync.RWMutex
	roles map[string]*role
}

type role struct {
	features map[string]bool
}

// Realm returns a handler that handles realm specific messages.
func Realm(name string) Handler {
	realmName := URI(name)
	if !realmName.Valid() {
		panic("invalid realm name")
	}
	return &realm{
		name: realmName,
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

	se, hasSession := SessionFromContext(ctx)
	if !hasSession {
		panic("router did not provide a session variable in the context")
	}

	switch m := msg.(type) {
	case *Hello:
		// Check if the session is already established with a realm
		if len(se.Realm) != 0 {
			handleProtocolViolation(ctx, conn)
			ctx, cancel := context.WithCancel(ctx)
			cancel()
			return NewErrorContext(ctx, fmt.Errorf("Protocol violation"))
		}

		// assign session to this  realm
		se.ID = NewGlobalID()
		se.Realm = r.name
		se.Details = m.Details

		welcomeMsg := &Welcome{WelcomeCode, se.ID, r.details()}
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
		se.Realm = URI("")
		se.Details = nil

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
