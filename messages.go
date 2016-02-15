//+build generate
package turms

//go:generate codecgen -o messages_codec.go messages.go

//go:generate stringer -type=MessageType
type MessageType uint32

const (
	HelloCode   MessageType = 1
	WelcomeCode MessageType = 2
	AbortCode   MessageType = 3
	GoodbyeCode MessageType = 6

	ErrorCode MessageType = 8

	PublishCode   MessageType = 16
	PublishedCode MessageType = 17

	SubscribeCode    MessageType = 32
	SubscribedCode   MessageType = 33
	UnsubscribeCode  MessageType = 34
	UnsubscribedCode MessageType = 35
	EventCode        MessageType = 36

	CallCode   MessageType = 48
	ResultCode MessageType = 50

	RegisterCode     MessageType = 64
	RegisteredCode   MessageType = 65
	UnregisterCode   MessageType = 66
	UnregisteredCode MessageType = 67
	InvocationCode   MessageType = 68
	YieldCode        MessageType = 70

	ChallengeCode    MessageType = 4
	AuthenticateCode MessageType = 5
	CancelCode       MessageType = 49
	InterruptCode    MessageType = 69
)

func NewMessage(t MessageType) Message {
	switch t {
	case HelloCode:
		return &Hello{T: t}
	case WelcomeCode:
		return &Welcome{T: t}
	case AbortCode:
		return &Abort{T: t}
	case GoodbyeCode:
		return &Goodbye{T: t}
	case ErrorCode:
		return &Error{T: t}
	case PublishCode:
		return &Publish{T: t}
	case PublishedCode:
		return &Published{T: t}

	case SubscribeCode:
		return &Subscribe{T: t}
	case SubscribedCode:
		return &Subscribed{T: t}
	case UnsubscribeCode:
		return &Unsubscribe{T: t}
	case UnsubscribedCode:
		return &Unsubscribed{T: t}
	case EventCode:
		return &Event{T: t}

	case CallCode:
		return &Call{T: t}
	case ResultCode:
		return &Result{T: t}

	case RegisterCode:
		return &Register{T: t}
	case RegisteredCode:
		return &Registered{T: t}
	case UnregisterCode:
		return &Unregister{T: t}
	case UnregisteredCode:
		return &Unregistered{T: t}
	case InvocationCode:
		return &Invocation{T: t}
	case YieldCode:
		return &Yield{T: t}

	case ChallengeCode:
		return nil
	case AuthenticateCode:
		return nil
	case CancelCode:
		return nil
	case InterruptCode:
		return nil

	}
	return nil
}

type Message interface {
	Type() MessageType
}

type Response interface {
	Message
	Response() ID
}

type Hello struct {
	T       MessageType
	Realm   URI
	Details map[string]interface{}
}

func (msg *Hello) Type() MessageType {
	return msg.T
}

type Welcome struct {
	T       MessageType
	Session ID
	Details map[string]interface{}
}

func (msg *Welcome) Type() MessageType {
	return msg.T
}

type Abort struct {
	T       MessageType
	Details map[string]interface{}
	Reason  URI
}

func (msg *Abort) Type() MessageType {
	return msg.T
}

type Goodbye struct {
	T       MessageType
	Details map[string]interface{}
	Reason  URI
}

func (msg *Goodbye) Type() MessageType {
	return msg.T
}

type Subscribe struct {
	T       MessageType
	Request ID
	Options map[string]interface{}
	Topic   URI
}

func (msg *Subscribe) Type() MessageType {
	return msg.T
}

type Subscribed struct {
	T            MessageType
	Request      ID
	Subscription ID
}

func (msg *Subscribed) Type() MessageType {
	return msg.T
}

func (sub *Subscribed) Response() ID {
	return sub.Request
}

type Error struct {
	T       MessageType
	ErrCode MessageType
	Request ID
	Details map[string]interface{}
	Error   URI
	Args    []interface{}
	ArgsKW  map[string]interface{}
}

func (msg *Error) Type() MessageType {
	return msg.T
}

func (err *Error) Response() ID {
	return err.Request
}

type Unsubscribe struct {
	T            MessageType
	Request      ID
	Subscription ID
}

func (msg *Unsubscribe) Type() MessageType {
	return msg.T
}

type Unsubscribed struct {
	T       MessageType
	Request ID
}

func (msg *Unsubscribed) Type() MessageType {
	return msg.T
}

func (unsub *Unsubscribed) Response() ID {
	return unsub.Request
}

type Publish struct {
	T       MessageType
	Request ID
	Options map[string]interface{}
	Topic   URI
	Args    []interface{}
	ArgsKW  map[string]interface{}
}

func (msg *Publish) Type() MessageType {
	return msg.T
}

type Published struct {
	T           MessageType
	Request     ID
	Publication ID
}

func (msg *Published) Type() MessageType {
	return msg.T
}

func (p *Published) Response() ID {
	return p.Request
}

type Event struct {
	T            MessageType
	Subscription ID
	Publication  ID
	Details      map[string]interface{}
	Args         []interface{}
	ArgsKW       map[string]interface{}
}

func (msg *Event) Type() MessageType {
	return msg.T
}

type Register struct {
	T         MessageType
	Request   ID
	Options   map[string]interface{}
	Procedure URI
}

func (msg *Register) Type() MessageType {
	return msg.T
}

type Registered struct {
	T            MessageType
	Request      ID
	Registration ID
}

func (msg *Registered) Type() MessageType {
	return msg.T
}

func (reg *Registered) Response() ID {
	return reg.Request
}

type Unregister struct {
	T            MessageType
	Request      ID
	Registration ID
}

func (msg *Unregister) Type() MessageType {
	return msg.T
}

type Unregistered struct {
	T       MessageType
	Request ID
}

func (msg *Unregistered) Type() MessageType {
	return msg.T
}

func (unreg *Unregistered) Response() ID {
	return unreg.Request
}

type Call struct {
	T         MessageType
	Request   ID
	Options   map[string]interface{}
	Procedure URI
	Args      []interface{}
	ArgsKW    map[string]interface{}
}

func (msg *Call) Type() MessageType {
	return msg.T
}

type Invocation struct {
	T            MessageType
	Request      ID
	Registration ID
	Details      map[string]interface{}
	Args         []interface{}
	ArgsKW       map[string]interface{}
}

func (msg *Invocation) Type() MessageType {
	return msg.T
}

type Yield struct {
	T       MessageType
	Request ID
	Options map[string]interface{}
	Args    []interface{}
	ArgsKW  map[string]interface{}
}

func (msg *Yield) Type() MessageType {
	return msg.T
}

func (y *Yield) Response() ID {
	return y.Request
}

type Result struct {
	T       MessageType
	Request ID
	Details map[string]interface{}
	Args    []interface{}
	ArgsKW  map[string]interface{}
}

func (msg *Result) Type() MessageType {
	return msg.T
}

func (res *Result) Response() ID {
	return res.Request
}
