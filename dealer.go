package turms

import (
	"fmt"
	"golang.org/x/net/context"
	"sync"
)

type dealer struct {
	// registrationID->SessionID
	endpoints   map[ID]map[ID]Conn
	procedures  map[URI]ID
	calls       map[ID]ID
	invocations map[ID]Conn
	mu          sync.RWMutex
}

// Dealer returns a handler that handles messages by routing calls
// from incoming Callers to Callees implementing the procedure called,
// and route call results back from Callees to Callers.
// Requires a Realm handler chained before the Dealer.
func Dealer() Handler {
	return &dealer{
		endpoints:   make(map[ID]map[ID]Conn),
		procedures:  make(map[URI]ID),
		calls:       make(map[ID]ID),
		invocations: make(map[ID]Conn),
	}
}

func (d *dealer) registerEndpoint(procedure URI, sessionID ID, conn Conn) (ID, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	procedureID, exist := d.procedures[procedure]
	if exist {
		return 0, &ProcedureAlreadyExists
	}
	procedureID = ID(0)
	d.procedures[procedure] = procedureID
	d.endpoints[procedureID] = map[ID]Conn{
		sessionID: conn,
	}
	return procedureID, nil
}

func (d *dealer) unregisterEndpoint(procedureID ID, sessionID ID, conn Conn) error {
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
		registrationID, err := d.registerEndpoint(m.Procedure, se.ID(), conn)
		if err != nil {
			errMsg := &Error{RegisterCode, m.Request, map[string]interface{}{}, URI("wamp.error.procedure_already_exists"), nil, nil}
			err = conn.Send(ctx, errMsg)
			if err != nil {
				return NewErrorContext(ctx, err)
			}
			return NewErrorContext(ctx, err)
		}
		registeredMsg := &Registered{m.Request, registrationID}
		err = conn.Send(ctx, registeredMsg)
		if err != nil {
			return NewErrorContext(ctx, err)
		}
	case *Unregister:
		err := d.unregisterEndpoint(m.Registration, se.ID(), conn)
		if err != nil {
			errMsg := &Error{UnregisterCode, m.Request, map[string]interface{}{}, NoSuchRegistration, nil, nil}
			if err == &NotAuthorized {
				errMsg.Error = NotAuthorized
			}
			err = conn.Send(ctx, errMsg)
			if err != nil {
				return NewErrorContext(ctx, err)
			}
			return NewErrorContext(ctx, err)
		}
		unregisteredMsg := &Unregistered{m.Request}
		err = conn.Send(ctx, unregisteredMsg)
		if err != nil {
			return NewErrorContext(ctx, err)
		}
	case *Call:
		endpoint, procedureID, err := d.getEndpoint(m.Procedure)
		if err != nil {
			errMsg := &Error{CallCode, m.Request, map[string]interface{}{}, NoSuchRegistration, nil, nil}
			err = conn.Send(ctx, errMsg)
			if err != nil {
				return NewErrorContext(ctx, err)
			}
			return NewErrorContext(ctx, err)
		}
		invocationReqID := se.NextID()
		invocationMsg := &Invocation{invocationReqID, procedureID, map[string]interface{}{}, m.Args, m.ArgsKW}
		err = endpoint.Send(ctx, invocationMsg)
		if err != nil {
			return NewErrorContext(ctx, err)
		}

		d.invocations[invocationReqID] = conn
		d.calls[invocationReqID] = m.Request
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
		resMsg := &Result{callReqID, details, m.Args, m.ArgsKW}
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
		respMsg := &Error{CallCode, callReqID, m.Details, m.Error, m.Args, m.ArgsKW}
		err := caller.Send(ctx, respMsg)
		if err != nil {
			return NewErrorContext(ctx, err)
		}
	}
	return ctx
}
