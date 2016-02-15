package turms

import (
	"unicode"
)

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
