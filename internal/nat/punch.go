package nat

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

// ErrNoCandidates is returned by RaceConnect when given an empty list.
var ErrNoCandidates = errors.New("nat: no candidates to dial")

// RaceConnect races TCP dials to all candidates in parallel and returns the
// first one that succeeds, along with the candidate string of the winner.
// Other in-flight attempts are cancelled, and any late-arriving winners are
// closed (no FD leaks).
//
// `timeout` bounds the whole operation. If no candidate succeeds within it,
// returns an error.
//
// What this is: a simple multi-candidate dial. Same-LAN works via the LAN-IP
// candidate; cross-internet works when the listening side is on a public IP
// (or has port forwarding configured).
//
// What this is NOT: TCP simultaneous open / hole punching. That requires
// SO_REUSEPORT, coordinated timing through the relay, and only works for
// certain NAT types. Out of scope for this step.
func RaceConnect(ctx context.Context, candidates []string, timeout time.Duration) (net.Conn, string, error) {
	if len(candidates) == 0 {
		return nil, "", ErrNoCandidates
	}

	attemptCtx, cancelAll := context.WithTimeout(ctx, timeout)

	type result struct {
		conn net.Conn
		addr string
		err  error
	}
	results := make(chan result, len(candidates))

	for _, c := range candidates {
		c := c
		go func() {
			conn, err := (&net.Dialer{}).DialContext(attemptCtx, "tcp", c)
			results <- result{conn, c, err}
		}()
	}

	var lastErr error
	for i := 0; i < len(candidates); i++ {
		r := <-results
		if r.err == nil {
			// We have a winner. Cancel remaining attempts and drain their
			// results in the background, closing any late winners.
			cancelAll()
			remaining := len(candidates) - i - 1
			go func() {
				for j := 0; j < remaining; j++ {
					if late := <-results; late.conn != nil {
						late.conn.Close()
					}
				}
			}()
			return r.conn, r.addr, nil
		}
		lastErr = r.err
	}
	cancelAll()
	return nil, "", fmt.Errorf("nat: all %d dial attempts failed; last error: %w", len(candidates), lastErr)
}
