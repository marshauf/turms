package service

import (
	"bytes"
	"fmt"
	"github.com/marshauf/turms"
	"golang.org/x/net/context"
	"reflect"
	"unicode"
	"unicode/utf8"
)

func newErrCol() errCol {
	return make(errCol, 0)
}

type errCol []error

func (e *errCol) IfErrAppend(err error) bool {
	if err != nil {
		*e = append(*e, err)
		return true
	}
	return false
}

func (e errCol) Error() string {
	if len(e) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if len(e) == 1 {
		buf.WriteString("1 error: ")
	} else {
		fmt.Fprintf(&buf, "%d errors: ", len(e))
	}

	for i, err := range e {
		if i != 0 {
			buf.WriteString("; ")
		}
		buf.WriteString(err.Error())
	}

	return buf.String()
}

type service struct {
	name       string
	procedures []string
	topics     []string
	c          *turms.Client
}

// Remove unregisters all service procedures and unsubscribes from all service topics.
func (s *service) Remove(ctx context.Context) error {
	res := make(chan error)

	for _, procedure := range s.procedures {
		namespace := turms.URI(s.name + "." + procedure)
		go func() {
			err := s.c.Unregister(ctx, namespace)
			if err != nil {
				res <- err
			}
		}()
	}
	for _, topic := range s.topics {
		namespace := turms.URI(s.name + "." + topic)
		go func() {
			err := s.c.Unsubscribe(ctx, namespace)
			if err != nil {
				res <- err
			}
		}()
	}

	errs := newErrCol()

	n := len(s.procedures) + len(s.topics)
	for i := 0; i < n; i++ {
		select {
		case err := <-res:
			errs.IfErrAppend(err)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// Is this an exported - upper case - name?
func isExported(name string) bool {
	rune, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(rune)
}

var typeOfError = reflect.TypeOf((*error)(nil)).Elem()

var (
	typeOfEvent = reflect.TypeOf(make(<-chan *turms.Event))
)

func New(ctx context.Context, c *turms.Client, rcvr interface{}, name string, useName bool) (*service, error) {
	srv := &service{c: c}
	typ := reflect.TypeOf(rcvr)
	val := reflect.ValueOf(rcvr)
	srv.name = reflect.Indirect(val).Type().Name()
	if name != "" {
		srv.name = name
	}
	if !isExported(srv.name) && !useName {
		return nil, fmt.Errorf("type %s is not exported", srv.name)
	}

	// Register methods as procedures
	srv.procedures = []string{}
	for m := 0; m < typ.NumMethod(); m++ {
		method := typ.Method(m)
		mtype := method.Type
		mname := method.Name
		// Method must be exported.
		if method.PkgPath != "" {
			continue
		}

		// Method needs at least one out to be a procedure
		numOut := mtype.NumOut()
		if numOut == 0 {
			continue
		}

		// Method last out must be error
		if errType := mtype.Out(numOut - 1); errType != typeOfError {
			continue
		}

		namespace := turms.URI(srv.name + "." + mname)
		f := func(ctx context.Context, inv *turms.Invocation) ([]interface{}, map[string]interface{}, error) {
			return nil, nil, nil
		}

		if err := c.Register(ctx, namespace, f); err != nil {
			return nil, err
		}
		srv.procedures = append(srv.procedures, mname)
	}

	// Subscribe to topics with the broker and send events through to channel fields
	srv.topics = []string{}
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		ftype := val.Type()
		fname := ftype.Name()
		// Field must be exported
		if len(ftype.PkgPath()) != 0 {
			continue
		}

		// Field must be of type chan *turms.Event
		if ftype != typeOfEvent {
			continue
		}

		namespace := turms.URI(srv.name + "." + fname)
		event, err := c.Subscribe(ctx, namespace)
		if err != nil {
			return nil, err
		}
		field.Set(reflect.ValueOf(event))
		srv.topics = append(srv.topics, fname)
	}
	return srv, nil
}
