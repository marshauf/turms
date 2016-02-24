package turms

import (
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/ugorji/go/codec"
	"golang.org/x/net/context"
	"sync"
	"time"
)

// A Conn represents a connection between two Peers.
type Conn interface {
	Read(context.Context) (Message, error)
	Send(context.Context, Message) error
	Close() error
}

func Pipe() (Conn, Conn) {
	c1 := make(chan Message)
	c2 := make(chan Message)
	return &pipe{
			c1, c2,
		}, &pipe{
			c2, c1,
		}
}

type pipe struct {
	in  chan Message
	out chan Message
}

func (p *pipe) Read(ctx context.Context) (Message, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg := <-p.in:
		return msg, nil
	}
}

func (p *pipe) Send(ctx context.Context, msg Message) error {
	select {
	case p.out <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *pipe) Close() error {
	close(p.out)
	return nil
}

const (
	subprotocolIdentifierJSON    = "wamp.2.json"
	subprotocolIdentifierMsgPack = "wamp.2.msgpack"
)

type WebsocketConn struct {
	c       *websocket.Conn
	msgType int
	dec     *codec.Decoder
	decmu   sync.Mutex
	enc     *codec.Encoder
	encmu   sync.Mutex
}

func NewWebsocketConn(c *websocket.Conn) (Conn, error) {
	var (
		subProtocol = c.Subprotocol()
		h           codec.Handle
		msgType     int
	)
	switch subProtocol {
	case subprotocolIdentifierJSON:
		h = &codec.JsonHandle{}
		msgType = websocket.TextMessage
	case subprotocolIdentifierMsgPack:
		h = &codec.MsgpackHandle{}
		msgType = websocket.BinaryMessage
	default:
		return nil, fmt.Errorf("Unsupported subprotocol %s", subProtocol)
	}
	return &WebsocketConn{
		c:       c,
		msgType: msgType,
		dec:     codec.NewDecoderBytes(nil, h),
		enc:     codec.NewEncoderBytes(nil, h),
	}, nil
}

func (c *WebsocketConn) Read(ctx context.Context) (Message, error) {
	res := make(chan msgAndErr, 1)
	c.decmu.Lock()
	defer c.decmu.Unlock()

	go func() {
		_, b, err := c.c.ReadMessage()
		if err != nil {
			res <- msgAndErr{msg: nil, err: err}
			return
		}
		c.dec.ResetBytes(b)
		var msgTyp [1]MessageType
		err = c.dec.Decode(&msgTyp)
		c.dec.ResetBytes(b)
		if err != nil {
			res <- msgAndErr{msg: nil, err: err}
			return
		}
		msg := NewMessage(msgTyp[0])
		err = c.dec.Decode(msg)
		res <- msgAndErr{msg: msg, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-res:
		return r.msg, r.err
	}
}

func (c *WebsocketConn) Send(ctx context.Context, msg Message) error {
	res := make(chan error, 1)

	go func() {
		c.encmu.Lock()
		defer c.encmu.Unlock()

		w, err := c.c.NextWriter(c.msgType)
		if err != nil {
			res <- err
		}
		defer w.Close()
		c.enc.Reset(w)

		res <- c.enc.Encode(msg)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-res:
		return err
	}
}

func (c *WebsocketConn) Close() error {
	return c.c.Close()
}

type msgAndErr struct {
	msg Message
	err error
}

func waitForMessage(parentCtx context.Context, c Conn, duration time.Duration) (Message, error) {
	ctx, cancel := context.WithTimeout(parentCtx, duration)
	defer cancel()
	return c.Read(ctx)
}
