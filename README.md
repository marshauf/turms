# Go WAMP implementation

## Spec implementation:

### Transports:
All message-based, reliable, ordered, bidirectional (full-duplex) connections which implement turms.Conn or net.Conn.

### Codec:
Implemented:
+ JSON
Missing:
- MsgPack 5+

### Basic Profile:
Implemented, test coverage low:
+ Sessions
+ Publisher
+ Subscriber
+ Caller
+ Callee

### Advanced Profile:
Missing:
- Progressive Call Results
- Progressive Calls
- Call Timeouts
- Call Canceling
- Caller Identification
- Call Trust Levels
- Registration Meta API
- Pattern-based Registrations
- Shared Registration
- Sharded Registration
- Registration Revocation
- Procedure Reflection
- Subscriber Black- and Whitelisting
- Publisher Exclusion
- Publisher Identification
- Publication Trust Levels
- Subscription Meta API
- Pattern-based Subscriptions
- Sharded Subscriptions
- Event History
- Registration Revocation
- Topic Reflection
- Session Meta API
- Authentication
