package turms

import (
	"io"
	"math/rand"
	"reflect"
	"sync"
	"unicode"
)

type Codec interface {
	Encode(io.Writer, Message) error
	Decode(io.Reader) (Message, error)
}

type URI string

func (u *URI) Error() string {
	return string(*u)
}

// Valid checks the URI for invalid patterns.
// Pattern: ^([0-9a-z_]+\.)*([0-9a-z_]+)$
func (u URI) Valid() bool {
	if len(u) == 0 {
		return false
	}
	if u[0] == '.' {
		return false
	}
	if u[len(u)-1] == '.' {
		return false
	}

	lastDot := false
	for _, c := range u {
		if c == '.' {
			if lastDot {
				return false
			}
			lastDot = true
			continue
		}
		lastDot = false
		if unicode.IsDigit(c) || unicode.IsLetter(c) || c == '_' {
			continue
		}
		return false
	}

	return true
}

var (
	InvalidURI             = URI("wamp.error.invalid_uri")                   // The provided URI is not valid.
	NoSuchProcedure        = URI("wamp.error.no_such_procedure")             // No procedure registered under the given URL.
	ProcedureAlreadyExists = URI("wamp.error.procedure_already_exists")      // A procedure with the given URI is already registered.
	NoSuchRegistration     = URI("wamp.error.no_such_registration")          // A Dealer could not perform an unregister, since the given registration is not active.
	NoSuchSubscription     = URI("wamp.error.no_such_subscription")          // A Broker could not perform an unsubscribe, since the given subscription is not active.
	InvalidArument         = URI("wamp.error.invalid_argument")              // A Call failed since the given argument types or values are not acceptable to the called procedure.
	SystemShutdown         = URI("wamp.error.system_shutdown")               // Used as a GOODBYE or ABORT reason.
	CloseRealm             = URI("wamp.error.close_realm")                   // Used as a GOODBYE reason when a Peer wants to leave the realm.
	GoodbyeAndOut          = URI("wamp.error.goodbye_and_out")               // Used as a GOODBYE reply reason to acknowledge the end of a Peers session.
	NotAuthorized          = URI("wamp.error.not_authorized")                // Peer is not authorized to perform the operation.
	AuthorizationFailed    = URI("wamp.error.authorization_failed")          // Dealer or Broker could not determine if the Peer is authorized to perform the operation.
	NoSuchRealm            = URI("wamp.error.no_such_realm")                 // Peer wanted to join non-exsisting realm.
	NoSuchRole             = URI("wamp.error.no_such_role")                  // Authenticated Role does not or no longer exists.
	Canceled               = URI("wamp.error.canceled")                      // Previously issued call is canceled.
	OptionNotAllowed       = URI("wamp.error.option_not_allowed")            // Requested interaction with an option was disallowed by the Router.
	NoEligibleCallee       = URI("wamp.error.no_eligible_callee")            // Callee black list or Caller exclusion lead to an exclusion of any Callee providing the procedure.
	DiscloseMe             = URI("wamp.error.option_disallowed.disclose_me") // A Router rejected client request to disclose its identity.
	NetworkFailure         = URI("wamp.error.network_failure")               // A Router encountered a network failure.
)

type ID uint64

const (
	upperBoundID = 9007199254740992
)

// NewGlobalID draws a randomly, uniformly distributed ID from the complete range of [0, 2^53].
// The drawn ID is allowed to be used in the global scope.
func NewGlobalID() ID {
	return ID(rand.Int63n(upperBoundID))
}

type idCounter struct {
	id ID
	mu sync.Mutex
}

func (idc *idCounter) Next() ID {
	idc.mu.Lock()
	defer idc.mu.Unlock()
	id := idc.id
	v := uint64(idc.id) + 1
	if v == upperBoundID {
		v = 1
	}
	idc.id = ID(v)
	return id
}

//go:generate stringer -type=Code
type Code uint32

var (
	messages = map[Code]reflect.Type{
		// Realm
		HelloCode:   reflect.TypeOf(Hello{}),
		WelcomeCode: reflect.TypeOf(Welcome{}),
		AbortCode:   reflect.TypeOf(Abort{}),
		GoodbyeCode: reflect.TypeOf(Goodbye{}),

		ErrorCode: reflect.TypeOf(Error{}),

		// Publisher
		PublishCode:   reflect.TypeOf(Publish{}),
		PublishedCode: reflect.TypeOf(Published{}),

		// Subscriber
		SubscribeCode:    reflect.TypeOf(Subscribe{}),
		SubscribedCode:   reflect.TypeOf(Subscribed{}),
		UnsubscribeCode:  reflect.TypeOf(Unsubscribe{}),
		UnsubscribedCode: reflect.TypeOf(Unsubscribed{}),
		EventCode:        reflect.TypeOf(Event{}),

		// Caller
		CallCode:   reflect.TypeOf(Call{}),
		ResultCode: reflect.TypeOf(Result{}),

		// Callee
		RegisterCode:     reflect.TypeOf(Register{}),
		RegisteredCode:   reflect.TypeOf(Registered{}),
		UnregisterCode:   reflect.TypeOf(Unregister{}),
		UnregisteredCode: reflect.TypeOf(Unregistered{}),
		InvocationCode:   reflect.TypeOf(Invocation{}),
		YieldCode:        reflect.TypeOf(Yield{}),
	}
)

func (c Code) Type() reflect.Type {
	t, ok := messages[c]
	if !ok {
		return reflect.TypeOf(nil)
	}
	return t
}

const (
	HelloCode   Code = 1
	WelcomeCode Code = 2
	AbortCode   Code = 3
	GoodbyeCode Code = 6

	ErrorCode Code = 8

	PublishCode   Code = 16
	PublishedCode Code = 17

	SubscribeCode    Code = 32
	SubscribedCode   Code = 33
	UnsubscribeCode  Code = 34
	UnsubscribedCode Code = 35
	EventCode        Code = 36

	CallCode   Code = 48
	ResultCode Code = 50

	RegisterCode     Code = 64
	RegisteredCode   Code = 65
	UnregisterCode   Code = 66
	UnregisteredCode Code = 67
	InvocationCode   Code = 68
	YieldCode        Code = 70

	ChallengeCode    Code = 4
	AuthenticateCode Code = 5
	CancelCode       Code = 49
	InterruptCode    Code = 69
)

type Message interface {
	Code() Code
}

type Response interface {
	Message
	Response() ID
}

type Hello struct {
	Realm   URI
	Details map[string]interface{}
}

func (_ *Hello) Code() Code {
	return HelloCode
}

type Welcome struct {
	Session ID
	Details map[string]interface{}
}

func (_ *Welcome) Code() Code {
	return WelcomeCode
}

type Abort struct {
	Details map[string]interface{}
	Reason  URI
}

func (_ *Abort) Code() Code {
	return AbortCode
}

type Goodbye struct {
	Details map[string]interface{}
	Reason  URI
}

func (_ *Goodbye) Code() Code {
	return GoodbyeCode
}

type Subscribe struct {
	Request ID
	Options map[string]interface{}
	Topic   URI
}

func (_ *Subscribe) Code() Code {
	return SubscribeCode
}

type Subscribed struct {
	Request      ID
	Subscription ID
}

func (_ *Subscribed) Code() Code {
	return SubscribedCode
}

func (s *Subscribed) Response() ID {
	return s.Request
}

type Error struct {
	ErrCode Code
	Request ID
	Details map[string]interface{}
	Error   URI
	Args    []interface{}
	ArgsKW  map[string]interface{}
}

func (_ *Error) Code() Code {
	return ErrorCode
}

func (e *Error) Response() ID {
	return e.Request
}

type Unsubscribe struct {
	Request      ID
	Subscription ID
}

func (_ *Unsubscribe) Code() Code {
	return UnsubscribeCode
}

type Unsubscribed struct {
	Request ID
}

func (_ *Unsubscribed) Code() Code {
	return UnsubscribedCode
}

func (u *Unsubscribed) Response() ID {
	return u.Request
}

type Publish struct {
	Request ID
	Options map[string]interface{}
	Topic   URI
	Args    []interface{}
	ArgsKW  map[string]interface{}
}

func (_ *Publish) Code() Code {
	return PublishCode
}

type Published struct {
	Request     ID
	Publication ID
}

func (_ *Published) Code() Code {
	return PublishedCode
}

func (p *Published) Response() ID {
	return p.Request
}

type Event struct {
	Subscription ID
	Publication  ID
	Details      map[string]interface{}
	Args         []interface{}
	ArgsKW       map[string]interface{}
}

func (_ *Event) Code() Code {
	return EventCode
}

type Register struct {
	Request   ID
	Options   map[string]interface{}
	Procedure URI
}

func (_ *Register) Code() Code {
	return RegisterCode
}

type Registered struct {
	Request      ID
	Registration ID
}

func (_ *Registered) Code() Code {
	return RegisteredCode
}

func (r *Registered) Response() ID {
	return r.Request
}

type Unregister struct {
	Request      ID
	Registration ID
}

func (_ *Unregister) Code() Code {
	return UnregisterCode
}

type Unregistered struct {
	Request ID
}

func (_ *Unregistered) Code() Code {
	return UnregisteredCode
}

func (u *Unregistered) Response() ID {
	return u.Request
}

type Call struct {
	Request   ID
	Options   map[string]interface{}
	Procedure URI
	Args      []interface{}
	ArgsKW    map[string]interface{}
}

func (_ *Call) Code() Code {
	return CallCode
}

type Invocation struct {
	Request      ID
	Registration ID
	Details      map[string]interface{}
	Args         []interface{}
	ArgsKW       map[string]interface{}
}

func (_ *Invocation) Code() Code {
	return InvocationCode
}

type Yield struct {
	Request ID
	Options map[string]interface{}
	Args    []interface{}
	ArgsKW  map[string]interface{}
}

func (_ *Yield) Code() Code {
	return YieldCode
}

func (y *Yield) Response() ID {
	return y.Request
}

type Result struct {
	Request ID
	Details map[string]interface{}
	Args    []interface{}
	ArgsKW  map[string]interface{}
}

func (_ *Result) Code() Code {
	return ResultCode
}

func (r *Result) Response() ID {
	return r.Request
}
