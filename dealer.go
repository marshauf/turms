package turms

import (
	"errors"
	"golang.org/x/net/context"
	"sync"
	"time"
)

var (
	EvOnCreate       = URI("wamp.registration.on_create")
	EvOnRegister     = URI("wamp.registration.on_register")
	EvOnUnregister   = URI("wamp.registration.on_unregister")
	EvOnDelete       = URI("wamp.registration.on_delete")
	ProcList         = URI("wamp.registration.list")
	ProcLookUp       = URI("wamp.registration.lookup")
	ProcMatch        = URI("wamp.registration.match")
	ProcGet          = URI("wamp.registration.get")
	ProcListCallees  = URI("wamp.registration.list_callees")
	ProcCountCallees = URI("wamp.registration.count_callees")

	ErrNoCaller = errors.New("caller does not exist")
	ErrNoCall   = errors.New("call does not exist")
)

type RegistrationDetails struct {
	ID               ID
	Created          time.Time
	Procedure        URI
	MatchPolicy      string
	InvocationPolicy string
}

type calleeReg struct {
	session ID
	conn    Conn
}

type registration struct {
	Callees []*calleeReg
	Details *RegistrationDetails
}

type dealer struct{}

// Dealer returns a handler that handles messages by routing calls
// from incoming Callers to Callees implementing the procedure called,
// and route call results back from Callees to Callers.
// Requires a Realm handler chained before the Dealer.
func Dealer() *dealer {
	return &dealer{}
}

type dealerStore struct {
	registrations map[ID]*registration
	procedures    map[URI]ID
	calls         map[ID]ID
	invocations   map[ID]Conn
	mu            sync.RWMutex
}

const (
	dealerStoreKey = "dealer"
)

func getDealerStore(r *Realm) *dealerStore {
	var store *dealerStore
	v, ok := r.Value(dealerStoreKey)
	if !ok {
		store = &dealerStore{
			registrations: make(map[ID]*registration),
			procedures:    make(map[URI]ID),
			calls:         make(map[ID]ID),
			invocations:   make(map[ID]Conn),
		}
		r.SetValue(dealerStoreKey, store)
	} else {
		store = v.(*dealerStore)
	}
	return store
}

func register(se *Session, conn Conn, req *Register) (ID, error) {
	r := se.Realm()
	store := getDealerStore(r)
	store.mu.Lock()
	defer store.mu.Unlock()
	registrationID, exist := store.procedures[req.Procedure]
	if exist {
		return 0, &ProcedureAlreadyExists
	}
	registrationID = se.RouterID()
	store.procedures[req.Procedure] = registrationID
	store.registrations[registrationID] = &registration{
		Callees: []*calleeReg{&calleeReg{se.ID(), conn}},
		Details: &RegistrationDetails{
			ID:               registrationID,
			Created:          time.Now(),
			Procedure:        req.Procedure,
			MatchPolicy:      "",
			InvocationPolicy: "",
		},
	}
	return registrationID, nil
}

func unregister(se *Session, req *Unregister) error {
	r := se.Realm()
	store := getDealerStore(r)
	store.mu.Lock()
	defer store.mu.Unlock()
	reg, exist := store.registrations[req.Registration]
	if !exist {
		return &NoSuchProcedure
	}

	valid := false
	for _, c := range reg.Callees {
		if c.session == se.ID() {
			valid = true
			break
		}
	}
	if !valid {
		return &NotAuthorized
	}

	delete(store.registrations, req.Registration)

	for name, id := range store.procedures {
		if id == req.Registration {
			delete(store.procedures, name)
			return nil
		}
	}
	panic("unreachable")
}

func getEndpoint(se *Session, procedure URI) (Conn, ID, error) {
	r := se.Realm()
	store := getDealerStore(r)
	store.mu.RLock()
	defer store.mu.RUnlock()
	procedureID, exist := store.procedures[procedure]
	if !exist {
		return nil, 0, &NoSuchProcedure
	}
	reg := store.registrations[procedureID]
	callee := reg.Callees[0]
	return callee.conn, procedureID, nil
}

func call(ctx context.Context, se *Session, conn Conn, req *Call) error {
	r := se.Realm()
	store := getDealerStore(r)
	store.mu.Lock()
	defer store.mu.Unlock()
	procedureID, exist := store.procedures[req.Procedure]
	if !exist {
		errMsg := &Error{ErrorCode, CallCode, req.Request, map[string]interface{}{}, NoSuchRegistration, nil, nil}
		return conn.Send(ctx, errMsg)
	}
	reg := store.registrations[procedureID]
	callee := reg.Callees[0]

	invocationReqID := se.SessionID()
	store.invocations[invocationReqID] = conn
	store.calls[invocationReqID] = req.Request
	invocationMsg := &Invocation{InvocationCode, invocationReqID, procedureID, map[string]interface{}{}, req.Args, req.ArgsKW}
	err := callee.conn.Send(ctx, invocationMsg)
	if err != nil {
		// TODO handle err
		// TODO drop endpoint depending on the error
		// TODO Send error message to caller
		delete(store.invocations, invocationReqID)
		delete(store.calls, invocationReqID)
	}
	return err
}

func yield(ctx context.Context, se *Session, req *Yield) error {
	r := se.Realm()
	store := getDealerStore(r)
	store.mu.RLock()
	defer store.mu.RUnlock()
	caller, hasCaller := store.invocations[req.Request]
	if !hasCaller {
		return ErrNoCaller
	}
	callReqID, hasCallID := store.calls[req.Request]
	if !hasCallID {
		return ErrNoCall
	}
	details := map[string]interface{}{}
	resMsg := &Result{ResultCode, callReqID, details, req.Args, req.ArgsKW}
	return caller.Send(ctx, resMsg)
}

func invocationError(ctx context.Context, se *Session, req *Error) error {
	r := se.Realm()
	store := getDealerStore(r)
	store.mu.RLock()
	defer store.mu.RUnlock()
	caller, hasCaller := store.invocations[req.Request]
	if !hasCaller {
		return ErrNoCaller
	}
	callReqID, hasCallID := store.calls[req.Request]
	if !hasCallID {
		return ErrNoCall
	}
	respMsg := &Error{ErrorCode, CallCode, callReqID, req.Details, req.Error, req.Args, req.ArgsKW}
	return caller.Send(ctx, respMsg)
}

func (d *dealer) Handle(ctx context.Context, conn Conn, msg Message) context.Context {
	se, hasSession := SessionFromContext(ctx)
	if !hasSession {
		return NewErrorContext(ctx, ErrNoSession)
	}

	switch m := msg.(type) {
	case *Register:
		registrationID, err := register(se, conn, m)
		if err != nil {
			errMsg := &Error{ErrorCode, RegisterCode, m.Request, map[string]interface{}{}, URI("wamp.error.procedure_already_exists"), nil, nil}
			err = conn.Send(ctx, errMsg)
			if err != nil {
				return NewErrorContext(ctx, err)
			}
			return NewErrorContext(ctx, err)
		}
		registeredMsg := &Registered{RegisteredCode, m.Request, registrationID}
		err = conn.Send(ctx, registeredMsg)
		if err != nil {
			return NewErrorContext(ctx, err)
		}
	case *Unregister:
		err := unregister(se, m)
		if err != nil {
			errMsg := &Error{ErrorCode, UnregisterCode, m.Request, map[string]interface{}{}, NoSuchRegistration, nil, nil}
			if err == &NotAuthorized {
				errMsg.Error = NotAuthorized
			}
			err = conn.Send(ctx, errMsg)
			if err != nil {
				return NewErrorContext(ctx, err)
			}
			return NewErrorContext(ctx, err)
		}
		unregisteredMsg := &Unregistered{UnregisteredCode, m.Request}
		err = conn.Send(ctx, unregisteredMsg)
		if err != nil {
			return NewErrorContext(ctx, err)
		}
	case *Call:
		if err := call(ctx, se, conn, m); err != nil {
			return NewErrorContext(ctx, err)
		}
	case *Yield:
		if err := yield(ctx, se, m); err != nil {
			return NewErrorContext(ctx, err)
		}
	case *Error:
		// Only handle Invocation errors
		if m.ErrCode != InvocationCode {
			return ctx
		}
		if err := invocationError(ctx, se, m); err != nil {
			return NewErrorContext(ctx, err)
		}
	}
	return ctx
}
