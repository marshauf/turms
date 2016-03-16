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
	ErrRealmExist        = errors.New("realm already exists")
	ErrRealmNoExist      = errors.New("realm doesn not exist")
)

// NewErrorContext returns a child context with the error value stored in it.
func NewErrorContext(ctx context.Context, err error) context.Context {
	return context.WithValue(ctx, errorContextKey, err)
}

// ErrorFromContext extracts the error value from the context.
func ErrorFromContext(ctx context.Context) (error, bool) {
	s, ok := ctx.Value(errorContextKey).(error)
	return s, ok
}

type Realm struct {
	name   URI
	mu     sync.RWMutex
	roles  map[string]*role
	conns  map[ID]Conn
	values map[interface{}]interface{}
}

func (r *Realm) SetValue(key interface{}, value interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values[key] = value
}

func (r *Realm) Value(key interface{}) (interface{}, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, exist := r.values[key]
	return value, exist
}

func (r *Realm) Name() URI {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.name
}

func (r *Realm) Details() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return map[string]interface{}{
		"roles": r.roles,
	}
}

type realmHandler struct {
	mu     sync.RWMutex
	realms map[URI]*Realm
}

type role struct {
	features map[string]bool
}

// NewRealm returns a realm handler that handles realm specific messages.
func NewRealm() *realmHandler {
	return &realmHandler{
		realms: make(map[URI]*Realm),
	}
}

func (h *realmHandler) CreateRealm(name string) error {
	u := URI(name)
	if !u.Valid() {
		return ErrInvalidURI
	}
	if _, exist := h.realms[u]; exist {
		return ErrRealmExist
	}
	h.realms[u] = &Realm{
		name:  u,
		conns: make(map[ID]Conn),
	}
	return nil
}

func (h *realmHandler) CloseRealm(name string) error {
	u := URI(name)
	if !u.Valid() {
		return ErrInvalidURI
	}
	if _, exist := h.realms[u]; !exist {
		return ErrRealmNoExist
	}
	delete(h.realms, u)
	return nil
}

func (h *realmHandler) Realm(name string) (*Realm, bool) {
	u := URI(name)
	r, exist := h.realms[u]
	return r, exist
}

func (r *Realm) RegisterRole(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, exist := r.roles[name]
	if exist {
		return ErrRoleExist
	}
	r.roles[name] = nil
	return nil
}

func (r *Realm) RegisterFeatures(name string, features ...string) error {
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
	// TODO realmHandler should mark message as a protocol violation and another middleware should decide what to do with the protocol violation
	// Check specs for a protocol violation documentation.
	defer conn.Close()
	byeMsg := &Goodbye{GoodbyeCode, map[string]interface{}{}, URI("wamp.error.protocol_violation")}
	return conn.Send(ctx, byeMsg)
}

func (h *realmHandler) Handle(ctx context.Context, conn Conn, msg Message) context.Context {
	se, hasSession := SessionFromContext(ctx)
	if !hasSession {
		panic("router did not provide a session variable in the context")
	}

	switch m := msg.(type) {
	case *Hello:
		// Check if the session is already established with a realm
		if se.realm != nil {
			handleProtocolViolation(ctx, conn)
			return NewErrorContext(ctx, ErrProtocolViolation)
		}

		h.mu.RLock()
		r, exist := h.realms[m.Realm]
		h.mu.RUnlock()
		if !exist {
			msg := &Abort{AbortCode, nil, NoSuchRealm}
			if err := conn.Send(ctx, msg); err != nil {
				return NewErrorContext(ctx, err)
			}
			return NewErrorContext(ctx, ErrRealmNoExist)
		}

		id := NewGlobalID()
		welcomeMsg := &Welcome{WelcomeCode, id, r.Details()}
		err := conn.Send(ctx, welcomeMsg)
		if err != nil {
			return NewErrorContext(ctx, err)
		}
		// assign session to this  realm
		se.id = id
		se.realm = r
		se.details = m.Details
		r.mu.Lock()
		r.conns[id] = conn
		r.mu.Unlock()
		return ctx
	case *Abort:
		if se.realm != nil {
			h.mu.RLock()
			r, exist := h.realms[se.realm.Name()]
			h.mu.RUnlock()
			if exist {
				r.mu.Lock()
				delete(r.conns, se.id)
				r.mu.Unlock()
			}
		}
		se.id = 0
		se.realm = nil
		se.details = nil
		return ctx
	case *Goodbye:
		// TODO when receiving a Goodbye message mark session in closing process
		if m.Reason != GoodbyeAndOut {
			// Reply with goodbye if received message is not a goodbye message
			byeMsg := &Goodbye{GoodbyeCode, map[string]interface{}{}, GoodbyeAndOut}
			err := conn.Send(ctx, byeMsg)
			if err != nil {
				ctx = NewErrorContext(ctx, err)
			}
		}
		h.mu.RLock()
		r, exist := h.realms[se.realm.Name()]
		h.mu.RUnlock()
		if exist {
			r.mu.Lock()
			delete(r.conns, se.id)
			r.mu.Unlock()
		}
		se.id = 0
		se.realm = nil
		se.details = nil
		return ctx
	}
	return ctx
}
