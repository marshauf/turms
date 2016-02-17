package turms

import (
	"github.com/ugorji/go/codec"
	"golang.org/x/net/context"
	"net"
	"time"
)

// A Conn represents a connection between two Peers.
type Conn interface {
	Read(context.Context) (Message, error)
	Send(context.Context, Message) error
	Close() error
}

type conn struct {
	conn net.Conn
	mr   *messageReader
	dec  *codec.Decoder
	enc  *codec.Encoder
}

// NewConn wraps a net.Conn
func NewConn(c net.Conn, h codec.Handle) Conn {
	mr := newMessageReader(c, 1024)
	// Force DecodeOptions.ErrorIfNoArrayExpand = false
	// and DecodeOptions.StructToArray = true
	switch handle := h.(type) {
	case *codec.JsonHandle:
		handle.DecodeOptions.ErrorIfNoArrayExpand = false
		handle.EncodeOptions.StructToArray = true
	case *codec.MsgpackHandle:
		handle.DecodeOptions.ErrorIfNoArrayExpand = false
		handle.EncodeOptions.StructToArray = true
	}
	return &conn{
		conn: c,
		mr:   mr,
		dec:  codec.NewDecoder(mr, h),
		enc:  codec.NewEncoder(c, h),
	}
}

func (c *conn) Close() error {
	return c.conn.Close()
}

func (c *conn) Send(ctx context.Context, msg Message) error {
	res := make(chan error, 1)
	go func() {
		err := c.enc.Encode(msg)
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
		var msgTyp [1]MessageType
		err := c.dec.Decode(&msgTyp)
		c.mr.ResetRead()
		if err != nil {
			res <- msgAndErr{msg: nil, err: err}
			return
		}
		msg := NewMessage(msgTyp[0])
		err = c.dec.Decode(msg)
		c.mr.Reset()
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
