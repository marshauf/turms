package turms

import (
	"errors"
	"github.com/gorilla/websocket"
	"golang.org/x/net/context"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotWS       = errors.New("not a websocket schema")
	ErrNoProcedure = errors.New("procedure does not exist")
)

// A Client is a Peer which connects to a Router.
type Client struct {
	conn         Conn
	ctx          context.Context
	ctxHandle    context.Context
	cancelHandle context.CancelFunc
	timeout      time.Duration
	counter      idCounter

	sessionID     ID
	serverDetails map[string]interface{}

	req *waitCondHandler
	sub *subscriber
	cal *callee

	mu       sync.RWMutex
	gdbyWait sync.Mutex
}

func Dial(url string) (*Client, error) {
	info := &DialInfo{
		URL:      url,
		Origin:   "http://localhost/",
		Protocol: "wamp.2.json",
		Timeout:  time.Second * 60,
	}
	return DialWithInfo(info)
}

type DialInfo struct {
	URL      string
	Origin   string
	Protocol string
	Timeout  time.Duration
}

func DialWithInfo(info *DialInfo) (*Client, error) {
	if !strings.HasPrefix(info.URL, "ws") {
		return nil, ErrNotWS
	}
	reqHeader := http.Header{"Origin": []string{info.Origin}}
	dialer := &websocket.Dialer{
		Subprotocols:     []string{info.Protocol},
		HandshakeTimeout: info.Timeout,
	}
	c, _, err := dialer.Dial(info.URL, reqHeader)
	if err != nil {
		return nil, err
	}
	ws, err := NewWebsocketConn(c)
	return NewClient(ws, info.Timeout), err
}

func NewClient(c Conn, timeout time.Duration) *Client {
	ctx := context.Background()
	ctxHandle, cancelHandle := context.WithCancel(ctx)
	return &Client{
		conn:         c,
		ctx:          ctx,
		ctxHandle:    ctxHandle,
		cancelHandle: cancelHandle,
		timeout:      timeout,
		sub:          newSubscriber(),
		cal:          newCallee(),
		req:          newWaitCondHandler(),
	}
}

type Details interface {
	Details() map[string]interface{}
}

type ClientDetails struct {
	Callee     bool
	Caller     bool
	Publisher  bool
	Subscriber bool
}

func (d *ClientDetails) Details() map[string]interface{} {
	details := map[string]interface{}{
		"roles": map[string]interface{}{},
	}
	if d.Callee {
		details["roles"].(map[string]interface{})["callee"] = map[string]interface{}{}
	}
	if d.Caller {
		details["roles"].(map[string]interface{})["caller"] = map[string]interface{}{}
	}
	if d.Publisher {
		details["roles"].(map[string]interface{})["publisher"] = map[string]interface{}{}
	}
	if d.Subscriber {
		details["roles"].(map[string]interface{})["subscriber"] = map[string]interface{}{}
	}
	return details
}

type PublishOption struct {
	AcknowledgePublish bool
}

func (opt *PublishOption) Options() map[string]interface{} {
	return map[string]interface{}{
		"acknowledge": opt.AcknowledgePublish,
	}
}

type Options interface {
	Options() map[string]interface{}
}

func (c *Client) JoinRealm(ctx context.Context, realm URI, details Details) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	msg := &Hello{HelloCode, realm, details.Details()}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	err := c.conn.Send(ctx, msg)
	if err != nil {
		return err
	}
	var (
		resp Message
	)
	resp, err = waitForMessage(ctx, c.conn, c.timeout)
	if err != nil {
		return err
	}

	switch m := resp.(type) {
	case *Welcome:
		c.serverDetails = m.Details
		c.sessionID = m.Session
	default:
		return ErrProtocolViolation
	}

	go c.handle()

	return nil
}

type waitMsg struct {
	msg Message
	c   chan struct{}
}

type waitCondHandler struct {
	requests map[ID]*waitMsg
	mu       sync.RWMutex
}

func newWaitCondHandler() *waitCondHandler {
	return &waitCondHandler{
		requests: make(map[ID]*waitMsg),
	}
}

func (h *waitCondHandler) wake(resp Response) {
	id := resp.Response()
	w, ok := h.requests[id]
	if !ok {
		return
	}
	w.msg = resp
	close(w.c)
}

func (h *waitCondHandler) set(reqID ID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	wm := &waitMsg{
		c: make(chan struct{}),
	}
	h.requests[reqID] = wm
}

func (h *waitCondHandler) wait(ctx context.Context, reqID ID) (Message, error) {
	h.mu.RLock()
	w, ok := h.requests[reqID]
	h.mu.RUnlock()
	if !ok {
		h.set(reqID)
		w = h.requests[reqID]
	}
	select {
	case <-w.c:
		return w.msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type subscriber struct {
	subscriptions map[ID]chan *Event
	topics        map[URI]ID
	mu            sync.RWMutex
}

func newSubscriber() *subscriber {
	return &subscriber{
		subscriptions: make(map[ID]chan *Event),
		topics:        make(map[URI]ID),
	}
}

func (s *subscriber) subscriptionID(topic URI) (ID, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.topics[topic]
	return id, ok
}

func (s *subscriber) subscribtion(sub ID) (chan *Event, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.subscriptions[sub]
	return c, ok
}

func (s *subscriber) unsubscribe(topic URI) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.topics[topic]
	if !ok {
		return ErrNoSub
	}
	close(s.subscriptions[sub])
	delete(s.topics, topic)
	delete(s.subscriptions, sub)
	return nil
}

func (s *subscriber) subscribe(sub ID, topic URI) chan *Event {
	c := make(chan *Event)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subscriptions[sub] = c
	s.topics[topic] = sub
	return c
}

func (c *Client) Subscribe(ctx context.Context, topic URI) (<-chan *Event, error) {
	reqID := c.counter.Next()

	c.req.set(reqID)

	req := &Subscribe{SubscribeCode, reqID, map[string]interface{}{}, URI(topic)}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := c.Send(ctx, req); err != nil {
		return nil, err
	}

	resp, err := c.req.wait(ctx, reqID)
	if err != nil {
		return nil, err
	}
	switch m := resp.(type) {
	case *Subscribed:
		p := c.sub.subscribe(m.Subscription, topic)
		return p, nil
	case *Error:
		return nil, &m.Error
	}
	return nil, ErrProtocolViolation
}

func (c *Client) Unsubscribe(ctx context.Context, topic URI) error {
	subID, exist := c.sub.subscriptionID(topic)
	if !exist {
		return ErrNoSub
	}
	reqID := c.counter.Next()
	msg := &Unsubscribe{UnsubscribeCode, reqID, subID}

	c.req.set(reqID)
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := c.Send(ctx, msg); err != nil {
		return err
	}

	resp, err := c.req.wait(ctx, reqID)
	if err != nil {
		return err
	}
	switch m := resp.(type) {
	case *Unsubscribed:
		return c.sub.unsubscribe(topic)
	case *Error:
		return &m.Error
	}
	return ErrProtocolViolation
}

func (c *Client) Publish(ctx context.Context, options Options, topic URI, args []interface{}, argsKW map[string]interface{}) error {
	reqID := c.counter.Next()
	if options == nil {
		options = &PublishOption{}
	}
	msg := &Publish{PublishCode, reqID, options.Options(), topic, args, argsKW}

	if options == nil {
		return c.Send(ctx, msg)
	}
	if a, ok := msg.Options["acknowledge"]; ok {
		if ac, ok := a.(bool); ok {
			if !ac {
				return c.Send(ctx, msg)
			}
		}
	}

	c.req.set(reqID)
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := c.Send(ctx, msg); err != nil {
		return err
	}

	resp, err := c.req.wait(ctx, reqID)
	if err != nil {
		return err
	}
	switch m := resp.(type) {
	case *Published:
		return nil
	case *Error:
		return &m.Error
	}
	return ErrProtocolViolation
}

type InvocationHandler func(context.Context, *Invocation) ([]interface{}, map[string]interface{}, error)

type callee struct {
	registrations map[ID]InvocationHandler
	procedures    map[URI]ID
	mu            sync.RWMutex
}

func newCallee() *callee {
	return &callee{
		registrations: make(map[ID]InvocationHandler),
		procedures:    make(map[URI]ID),
	}
}

func (c *callee) register(regID ID, name URI, h InvocationHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.registrations[regID] = h
	c.procedures[name] = regID
}

func (c *callee) unregister(name URI) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	regID, ok := c.procedures[name]
	if !ok {
		return ErrNoProcedure
	}
	delete(c.procedures, name)
	delete(c.registrations, regID)
	return nil
}

func (c *callee) registrationID(name URI) (ID, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	id, ok := c.procedures[name]
	return id, ok
}

func (c *callee) handler(id ID) (InvocationHandler, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	h, ok := c.registrations[id]
	return h, ok
}

func (c *Client) Register(ctx context.Context, name URI, h InvocationHandler) error {
	reqID := c.counter.Next()
	options := map[string]interface{}{}
	msg := &Register{RegisterCode, reqID, options, name}

	c.req.set(reqID)
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := c.Send(ctx, msg); err != nil {
		return err
	}

	resp, err := c.req.wait(ctx, reqID)
	if err != nil {
		return err
	}
	switch m := resp.(type) {
	case *Registered:
		c.cal.register(m.Registration, name, h)
		return nil
	case *Error:
		return &m.Error
	}
	return ErrProtocolViolation
}

func (c *Client) Unregister(ctx context.Context, name URI) error {
	procedureID, exist := c.cal.registrationID(name)
	if !exist {
		return ErrNoProcedure
	}
	reqID := c.counter.Next()
	msg := &Unregister{UnregisterCode, reqID, procedureID}

	c.req.set(reqID)
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := c.Send(ctx, msg); err != nil {
		return err
	}

	resp, err := c.req.wait(ctx, reqID)
	if err != nil {
		return err
	}
	switch m := resp.(type) {
	case *Unregistered:
		return c.cal.unregister(name)
	case *Error:
		return &m.Error
	}
	return ErrProtocolViolation
}

func (c *Client) Call(ctx context.Context, procedure URI, args []interface{}, argsKW map[string]interface{}) (*Result, error) {
	reqID := c.counter.Next()

	options := map[string]interface{}{}
	msg := &Call{CallCode, reqID, options, procedure, args, argsKW}

	c.req.set(reqID)
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := c.Send(ctx, msg); err != nil {
		return nil, err
	}

	resp, err := c.req.wait(ctx, reqID)
	if err != nil {
		return nil, err
	}
	switch m := resp.(type) {
	case *Result:
		return m, nil
	case *Error:
		return nil, &m.Error
	}
	return nil, ErrProtocolViolation
}

func (c *Client) Send(ctx context.Context, message Message) error {
	return c.conn.Send(ctx, message)
}

func (c *Client) handle() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			msg, err := waitForMessage(c.ctx, c.conn, c.timeout)
			switch err {
			case context.Canceled, context.DeadlineExceeded:
				return
			default:
				// TODO store error value
				c.cancelHandle()
				return
			case nil:
			}

			switch m := msg.(type) {
			case *Welcome:
				// TODO Protocol violation
			case *Goodbye:
				switch m.Reason {
				case GoodbyeAndOut:
					// Received goodbye confirmation
					// TODO check if request to close the session came from the client or router
					c.gdbyWait.Unlock()
				default:
					// router send a goodbye message
					ctx, _ := context.WithTimeout(c.ctx, c.timeout)
					c.conn.Send(ctx, msg) // omitting err value because closing session anyway
				}
				return
			case *Abort:
				// TODO Protocol violation
			case *Event:
				if sub, ok := c.sub.subscribtion(m.Subscription); ok {
					select {
					case sub <- m:
					case <-time.Tick(c.timeout):
					}
				}
			case *Invocation:
				reg, exist := c.cal.handler(m.Registration)
				if !exist {
					details := map[string]interface{}{}
					resp := &Error{ErrorCode, InvocationCode, m.Request, details, NoSuchProcedure, nil, nil}
					err := c.Send(c.ctx, resp)
					switch err {
					case context.Canceled, context.DeadlineExceeded:
						return
					default:
						// TODO store error value
						c.cancelHandle()
						return
					case nil:
						continue
					}
				}
				args, argsKW, err := reg(c.ctx, m)
				if err != nil {
					details := map[string]interface{}{}
					resp := &Error{ErrorCode, InvocationCode, m.Request, details, URI(err.Error()), args, argsKW}
					err := c.Send(c.ctx, resp)
					switch err {
					case context.Canceled, context.DeadlineExceeded:
						return
					default:
						// TODO store error value
						c.cancelHandle()
						return
					case nil:
						continue
					}
				}
				details := map[string]interface{}{}
				resp := &Yield{YieldCode, m.Request, details, args, argsKW}
				err = c.Send(c.ctx, resp)
				switch err {
				case context.Canceled, context.DeadlineExceeded:
					return
				default:
					// TODO store error value
					c.cancelHandle()
					return
				case nil:
					continue
				}
			default:
				// TODO How does a client handle unknown messages
			}

			if resp, ok := msg.(Response); ok {
				c.req.wake(resp)
			}
		}
	}
}

func (c *Client) Close() error {
	c.gdbyWait.Lock()

	gdbye := &Goodbye{GoodbyeCode, map[string]interface{}{}, CloseRealm}
	ctx, cancel := context.WithTimeout(c.ctx, c.timeout)
	c.conn.Send(ctx, gdbye)
	cancel()

	c.gdbyWait.Lock()
	c.gdbyWait.Unlock()
	c.cancelHandle()
	return c.conn.Close()
}
