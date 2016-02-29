package turms

import (
	"fmt"
	"golang.org/x/net/context"
	"sync"
	"time"
)

type RegistrationDetails struct {
	ID               ID
	Created          time.Time
	Procedure        URI
	MatchPolicy      string
	InvocationPolicy string
}

type Dealer interface {
	Register(ctx context.Context, procedure URI, session *Session, conn Conn) (ID, error)
	Unregister(ctx context.Context, procedure ID, session ID) error

	Registrations(ctx context.Context) (exact []ID, prefix []ID, wildcard []ID)
	Registration(ctx context.Context, procedure URI, options map[string]interface{}) (registration ID, exists bool)
	RegistrationDetails(ctx context.Context, registration ID) (*RegistrationDetails, error)
	Callees(ctx context.Context, registration ID) ([]ID, error)
}

type dealer struct {
	// TODO Use RegistrationDetails
	// registrationID->SessionID
	endpoints   map[ID]map[ID]Conn
	procedures  map[URI]ID
	calls       map[ID]ID
	invocations map[ID]Conn
	mu          sync.RWMutex
}

// NewDealer returns a handler that handles messages by routing calls
// from incoming Callers to Callees implementing the procedure called,
// and route call results back from Callees to Callers.
// Requires a Realm handler chained before the Dealer.
func NewDealer() *dealer {
	return &dealer{
		endpoints:   make(map[ID]map[ID]Conn),
		procedures:  make(map[URI]ID),
		calls:       make(map[ID]ID),
		invocations: make(map[ID]Conn),
	}
}

func (d *dealer) Register(ctx context.Context, procedure URI, session *Session, conn Conn) (ID, error) {
	return d.registerEndpoint(procedure, session.ID, session.routerIDGen, conn)
}

func (d *dealer) Unregister(ctx context.Context, procedure ID, session ID) error {
	return d.unregisterEndpoint(procedure, session)
}

// Registrations returns all registered procedures.
func (d *dealer) Registrations(ctx context.Context) (exact []ID, prefix []ID, wildcard []ID) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	exact = make([]ID, len(d.endpoints))
	prefix = []ID{}   // Not yet implemented
	wildcard = []ID{} // Not yet implemented
	i := 0
	for id := range d.endpoints {
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
	endpoints := d.endpoints[registration]
	callees := make([]ID, len(endpoints))
	i := 0
	for id := range endpoints {
		callees[i] = id
		i++
	}
	return callees, nil
}

func (d *dealer) registerEndpoint(procedure URI, sessionID ID, idC *idCounter, conn Conn) (ID, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	procedureID, exist := d.procedures[procedure]
	if exist {
		return 0, &ProcedureAlreadyExists
	}
	procedureID = idC.Next()
	d.procedures[procedure] = procedureID
	d.endpoints[procedureID] = map[ID]Conn{
		sessionID: conn,
	}
	return procedureID, nil
}

func (d *dealer) unregisterEndpoint(procedureID ID, sessionID ID) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	procedure, exist := d.endpoints[procedureID]
	if !exist {
		return &NoSuchProcedure
	}
	_, valid := procedure[sessionID]
	if !valid {
		return &NotAuthorized
	}
	delete(d.endpoints, procedureID)
	for key, value := range d.procedures {
		if value == procedureID {
			delete(d.procedures, key)
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
	endpoint := d.endpoints[procedureID]
	for sessionID := range endpoint {
		return endpoint[sessionID], procedureID, nil
	}
	// TODO This should not happen
	return nil, 0, &NetworkFailure
}

func (d *dealer) Handle(ctx context.Context, conn Conn, msg Message) context.Context {
	se, hasSession := SessionFromContext(ctx)
	if !hasSession {
		return NewErrorContext(ctx, fmt.Errorf("Broker requires a session stored in the context"))
	}

	switch m := msg.(type) {
	case *Register:
		registrationID, err := d.registerEndpoint(m.Procedure, se.ID, se.routerIDGen, conn)
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
		err := d.unregisterEndpoint(m.Registration, se.ID)
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
			return NewErrorContext(ctx, fmt.Errorf("No caller found"))
		}

		callReqID, hasCallID := d.calls[m.Request]
		if !hasCallID {
			return NewErrorContext(ctx, fmt.Errorf("No call ID found"))
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
			return NewErrorContext(ctx, fmt.Errorf("No caller found"))
		}

		callReqID, hasCallID := d.calls[m.Request]
		if !hasCallID {
			return NewErrorContext(ctx, fmt.Errorf("No call ID found"))
		}
		respMsg := &Error{ErrorCode, CallCode, callReqID, m.Details, m.Error, m.Args, m.ArgsKW}
		err := caller.Send(ctx, respMsg)
		if err != nil {
			return NewErrorContext(ctx, err)
		}
	}
	return ctx
}
