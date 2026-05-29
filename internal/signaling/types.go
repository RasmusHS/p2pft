package signaling

// Message type discriminators used in the Envelope.Type field.
// Keeping these as constants prevents typos drifting between sender and relay.
const (
	TypeSenderHello    = "sender_hello"
	TypeReceiverHello  = "receiver_hello"
	TypePeerAddrs      = "peer_addrs"
	TypeSessionCreated = "session_created"
	TypeSessionFound   = "session_found"
	TypeReceiverJoined = "receiver_joined"
	TypeSessionError   = "session_error"
)
