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

type Dealer interface {
	Register(ctx context.Context, procedure URI, clientSe *ClientSession, routerSe *RouterSession, conn Conn) (ID, error)
	Unregister(ctx context.Context, procedure ID, session ID) error

	Registrations(ctx context.Context) (exact []ID, prefix []ID, wildcard []ID)
	Registration(ctx context.Context, procedure URI, options map[string]interface{}) (registration ID, exists bool)
	RegistrationDetails(ctx context.Context, registration ID) (*RegistrationDetails, error)
	Callees(ctx context.Context, registration ID) ([]ID, error)
}

type calleeReg struct {
	session ID
	conn    Conn
}

type registration struct {
	Callees []*calleeReg
	Details *RegistrationDetails
}

type dealer struct {
	registrations map[ID]*registration
	procedures    map[URI]ID
	calls         map[ID]ID
	invocations   map[ID]Conn
	mu            sync.RWMutex
}

// NewDealer returns a handler that handles messages by routing calls
// from incoming Callers to Callees implementing the procedure called,
// and route call results back from Callees to Callers.
// Requires a Realm handler chained before the Dealer.
func NewDealer() *dealer {
	return &dealer{
		registrations: make(map[ID]*registration),
		procedures:    make(map[URI]ID),
		calls:         make(map[ID]ID),
		invocations:   make(map[ID]Conn),
	}
}

func (d *dealer) Register(ctx context.Context, procedure URI, clientSe *ClientSession, routerSe *RouterSession, conn Conn) (ID, error) {
	return d.register(procedure, clientSe.ID(), routerSe.gen, conn)
}

func (d *dealer) Unregister(ctx context.Context, procedure ID, session ID) error {
	return d.unregister(procedure, session)
}

// Registrations returns all registered procedures.
func (d *dealer) Registrations(ctx context.Context) (exact []ID, prefix []ID, wildcard []ID) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	exact = make([]ID, len(d.registrations))
	prefix = []ID{}   // Not yet implemented
	wildcard = []ID{} // Not yet implemented
	i := 0
	for id := range d.registrations {
		exact[i] = id
		i++
	}
	return
}

func (d *dealer) Registration(ctx context.Context, procedure URI, options map[string]interface{}) (registration ID, exists bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	registration, exists = d.procedures[procedure]
	return
}

func (d *dealer) RegistrationDetails(ctx context.Context, registration ID) (*RegistrationDetails, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	// TODO fill details
	details := &RegistrationDetails{
		ID: registration,
	}
	return details, nil
}

func (d *dealer) Callees(ctx context.Context, registration ID) ([]ID, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	callees := d.registrations[registration].Callees
	ids := make([]ID, len(callees))
	i := 0
	for _, c := range callees {
		ids[i] = c.session
		i++
	}
	return ids, nil
}

func (d *dealer) register(procedure URI, sessionID ID, idC *idCounter, conn Conn) (ID, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	registrationID, exist := d.procedures[procedure]
	if exist {
		return 0, &ProcedureAlreadyExists
	}
	registrationID = idC.Next()
	d.procedures[procedure] = registrationID
	d.registrations[registrationID] = &registration{
		Callees: []*calleeReg{&calleeReg{sessionID, conn}},
		Details: &RegistrationDetails{
			ID:               registrationID,
			Created:          time.Now(),
			Procedure:        procedure,
			MatchPolicy:      "",
			InvocationPolicy: "",
		},
	}
	return registrationID, nil
}

func (d *dealer) unregister(registrationID ID, sessionID ID) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	reg, exist := d.registrations[registrationID]
	if !exist {
		return &NoSuchProcedure
	}

	valid := false
	for _, c := range reg.Callees {
		if c.session == sessionID {
			valid = true
			break
		}
	}
	if !valid {
		return &NotAuthorized
	}

	delete(d.registrations, registrationID)

	for name, id := range d.procedures {
		if id == registrationID {
			delete(d.procedures, name)
			return nil
		}
	}
	return nil
}

func (d *dealer) getEndpoint(procedureURI URI) (Conn, ID, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	procedureID, exist := d.procedures[procedureURI]
	if !exist {
		return nil, 0, &NoSuchProcedure
	}
	reg := d.registrations[procedureID]
	callee := reg.Callees[0]
	return callee.conn, procedureID, nil
}

func (d *dealer) Handle(ctx context.Context, conn Conn, msg Message) context.Context {
	se, hasSession := ClientSessionFromContext(ctx)
	if !hasSession {
		return NewErrorContext(ctx, ErrNoSession)
	}
	rse, hasSession := RouterSessionFromContext(ctx)
	if !hasSession {
		return NewErrorContext(ctx, ErrNoSession)
	}

	switch m := msg.(type) {
	case *Register:
		registrationID, err := d.register(m.Procedure, se.ID(), rse.gen, conn)
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
		err := d.unregister(m.Registration, se.ID())
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
		endpoint, procedureID, err := d.getEndpoint(m.Procedure)
		if err != nil {
			errMsg := &Error{ErrorCode, CallCode, m.Request, map[string]interface{}{}, NoSuchRegistration, nil, nil}
			err = conn.Send(ctx, errMsg)
			if err != nil {
				return NewErrorContext(ctx, err)
			}
			return NewErrorContext(ctx, err)
		}
		invocationReqID := se.SessionID()
		d.invocations[invocationReqID] = conn
		d.calls[invocationReqID] = m.Request
		invocationMsg := &Invocation{InvocationCode, invocationReqID, procedureID, map[string]interface{}{}, m.Args, m.ArgsKW}
		err = endpoint.Send(ctx, invocationMsg)
		if err != nil {
			// TODO handle err
			// TODO drop endpoint depending on the error
			// TODO Send error message to caller
			delete(d.invocations, invocationReqID)
			delete(d.calls, invocationReqID)
			return NewErrorContext(ctx, err)
		}
	case *Yield:
		caller, hasCaller := d.invocations[m.Request]
		if !hasCaller {
			return NewErrorContext(ctx, ErrNoCaller)

		}

		callReqID, hasCallID := d.calls[m.Request]
		if !hasCallID {
			return NewErrorContext(ctx, ErrNoCall)
		}
		details := map[string]interface{}{}
		resMsg := &Result{ResultCode, callReqID, details, m.Args, m.ArgsKW}
		err := caller.Send(ctx, resMsg)
		if err != nil {
			return NewErrorContext(ctx, err)
		}
	case *Error:
		if m.ErrCode != InvocationCode {
			return ctx
		}
		caller, hasCaller := d.invocations[m.Request]
		if !hasCaller {
			return NewErrorContext(ctx, ErrNoCaller)
		}

		callReqID, hasCallID := d.calls[m.Request]
		if !hasCallID {
			return NewErrorContext(ctx, ErrNoCall)
		}
		respMsg := &Error{ErrorCode, CallCode, callReqID, m.Details, m.Error, m.Args, m.ArgsKW}
		err := caller.Send(ctx, respMsg)
		if err != nil {
			return NewErrorContext(ctx, err)
		}
	}
	return ctx
}
