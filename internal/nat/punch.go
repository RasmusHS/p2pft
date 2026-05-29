package nat

import (
	"context"
	"errors"
	"net"
)

// Candidate is one address either peer might be reachable at.
type Candidate struct {
	Network string // "tcp" or "udp"
	Addr    string // host:port
	Kind    string // "local" or "public" — informational, used for ordering
}

// ErrAllAttemptsFailed is returned when every candidate dial fails.
var ErrAllAttemptsFailed = errors.New("nat: all connection attempts failed")

// Punch races multiple connection attempts to the peer's candidate addresses
// and returns the first successful one. All other in-flight attempts are
// cancelled.
//
// For TCP: simple parallel dials. The peer is also dialing us simultaneously,
// which is what makes simultaneous-open work for some NAT types.
//
// For UDP: each side sends a packet to every candidate address, which opens
// a NAT mapping on the local side. Subsequent packets from the peer can
// then traverse that mapping. Detection of a "successful" UDP punch is
// receiving any valid packet back.
//
// TODO: implement. Suggested approach:
//  1. Spawn one goroutine per candidate. Each uses a derived ctx that can be
//     cancelled when a winner emerges.
//  2. Goroutines write their result (conn or error) to a shared chan.
//  3. Parent goroutine reads the first non-error result, cancels the derived
//     ctx, drains remaining results closing their conns.
func Punch(ctx context.Context, candidates []Candidate) (net.Conn, error) {
	_ = ctx
	_ = candidates
	return nil, ErrAllAttemptsFailed
}
