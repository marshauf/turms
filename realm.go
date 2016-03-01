package turms

import (
	"errors"
	"golang.org/x/net/context"
	"sync"
)

const (
	clientSessionContextKey = "clientsession"
	routerSessionContextKey = "routersession"
	errorContextKey         = "error"
)

var (
	ErrRoleExist         = errors.New("role already exists")
	ErrRoleNotExist      = errors.New("role does not exist")
	ErrProtocolViolation = errors.New("protocol violation")
)

type RouterSession struct {
	gen *idCounter
}

func (s *RouterSession) RouterID() ID {
	return s.gen.Next()
}

type RealmSession struct {
	mu sync.RWMutex
}

// ClientSession stores the values and optional configuration for a client session.
type ClientSession struct {
	id      ID
	realm   URI
	details map[string]interface{}
	gen     idCounter
	mu      sync.RWMutex
}

func (s *ClientSession) ID() ID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.id
}

// SessionID returns a new ID in the session scope.
func (s *ClientSession) SessionID() ID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gen.Next()
}

func (s *ClientSession) Details() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.details
}

func (s *ClientSession) Realm() URI {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.realm
}

// NewClientSessionContext returns a child context with the client session value stored in it.
func NewClientSessionContext(ctx context.Context, session *ClientSession) context.Context {
	return context.WithValue(ctx, clientSessionContextKey, session)
}

// ClientSessionFromContext extracts the session value from the context.
func ClientSessionFromContext(ctx context.Context) (*ClientSession, bool) {
	s, ok := ctx.Value(clientSessionContextKey).(*ClientSession)
	return s, ok
}

// NewRouterSessionContext returns a child context with the client session value stored in it.
func NewRouterContext(ctx context.Context, session *RouterSession) context.Context {
	return context.WithValue(ctx, routerSessionContextKey, session)
}

// RouterSessionFromContext extracts the session value from the context.
func RouterSessionFromContext(ctx context.Context) (*RouterSession, bool) {
	s, ok := ctx.Value(routerSessionContextKey).(*RouterSession)
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
		return ErrRoleExist
	}
	r.roles[name] = nil
	return nil
}

func (r *realm) RegisterFeatures(name string, features ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rl, exist := r.roles[name]
	if !exist {
		return ErrRoleNotExist
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
	se, hasSession := ClientSessionFromContext(ctx)
	if !hasSession {
		panic("router did not provide a session variable in the context")
	}

	switch m := msg.(type) {
	case *Hello:
		// Check if the session is already established with a realm
		if len(se.Realm()) != 0 {
			handleProtocolViolation(ctx, conn)
			ctx, cancel := context.WithCancel(ctx)
			cancel()
			return NewErrorContext(ctx, ErrProtocolViolation)
		}

		// assign session to this  realm
		se.id = NewGlobalID()
		se.realm = r.name
		se.details = m.Details

		welcomeMsg := &Welcome{WelcomeCode, se.ID(), r.details()}
		err := conn.Send(ctx, welcomeMsg)
		if err != nil {
			return NewErrorContext(ctx, err)
		}
		return ctx
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
		se.id = 0
		se.realm = URI("")
		se.details = nil

		ctx, cancel := context.WithCancel(ctx)
		cancel()
		return ctx
	}
	return ctx
}

func (r *realm) details() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return map[string]interface{}{
		"roles": r.roles,
	}
}
