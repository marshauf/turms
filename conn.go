package turms

import (
	"golang.org/x/net/context"
	"net"
	"sync"
	"time"
)

// A Conn represents a connection between two Peers.
type Conn interface {
	Read(context.Context) (Message, error)
	Send(context.Context, Message) error
	Close() error
}

type conn struct {
	conn  net.Conn
	codec Codec
	mu    sync.RWMutex
}

// NewConn wraps a net.Conn
func NewConn(c net.Conn, codec Codec) Conn {
	return &conn{
		conn:  c,
		codec: codec,
	}
}

func (c *conn) Close() error {
	return c.conn.Close()
}

func (c *conn) Send(ctx context.Context, msg Message) error {
	res := make(chan error, 1)
	go func() {
		err := c.codec.Encode(c.conn, msg)
		res <- err
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-res:
		return err
	}
}

func (c *conn) Read(ctx context.Context) (Message, error) {
	type msgAndErr struct {
		msg Message
		err error
	}
	res := make(chan msgAndErr, 1)
	go func() {
		msg, err := c.codec.Decode(c.conn)
		res <- msgAndErr{msg: msg, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-res:
		return r.msg, r.err
	}
}

func waitForMessage(parentCtx context.Context, c Conn, duration time.Duration) (Message, error) {
	ctx, cancel := context.WithTimeout(parentCtx, duration)
	defer cancel()
	return c.Read(ctx)
}
