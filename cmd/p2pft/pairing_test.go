package main

import (
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestPreambleRoundTrip: write a preamble on one end, read it on the other,
// verify the payload matches.
func TestPreambleRoundTrip(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	const want = "amber-forest-quartz"

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := writePreamble(a, want); err != nil {
			t.Errorf("writePreamble: %v", err)
		}
	}()

	got, err := readPreamble(b)
	if err != nil {
		t.Fatalf("readPreamble: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	wg.Wait()
}

// TestAcceptWithCodeMatch: a single sender writes the expected preamble;
// acceptWithCode returns that connection.
func TestAcceptWithCodeMatch(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	if tcpL, ok := l.(*net.TCPListener); ok {
		_ = tcpL.SetDeadline(time.Now().Add(5 * time.Second))
	}

	const code = "amber-forest-quartz"

	go func() {
		conn, err := net.Dial("tcp", l.Addr().String())
		if err != nil {
			return
		}
		_ = writePreamble(conn, code)
		// Leave the conn open so acceptWithCode's caller can use it.
		time.Sleep(2 * time.Second)
		conn.Close()
	}()

	conn, err := acceptWithCode(l, code)
	if err != nil {
		t.Fatalf("acceptWithCode: %v", err)
	}
	conn.Close()
}

// TestAcceptWithCodeSkipsLosers: simulate the same-host race — several
// connections arrive at the listener; only one writes the matching code.
// acceptWithCode must skip the others and return the matching one.
func TestAcceptWithCodeSkipsLosers(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	if tcpL, ok := l.(*net.TCPListener); ok {
		_ = tcpL.SetDeadline(time.Now().Add(5 * time.Second))
	}

	const code = "amber-forest-quartz"
	const wrong = "zebra-jasper-meadow"

	var wg sync.WaitGroup

	// Two "losers" — connect and either close immediately or write the
	// wrong code.
	for i := 0; i < 2; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			conn, err := net.Dial("tcp", l.Addr().String())
			if err != nil {
				return
			}
			if i == 0 {
				// Idle loser — closes without writing.
				conn.Close()
			} else {
				// Wrong-code loser — writes a different preamble.
				_ = writePreamble(conn, wrong)
				conn.Close()
			}
		}()
	}

	// The "winner" — connects and writes the correct code.
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Small delay so the losers are likely to be in the accept queue first.
		time.Sleep(50 * time.Millisecond)
		conn, err := net.Dial("tcp", l.Addr().String())
		if err != nil {
			return
		}
		_ = writePreamble(conn, code)
		time.Sleep(2 * time.Second)
		conn.Close()
	}()

	conn, err := acceptWithCode(l, code)
	if err != nil {
		t.Fatalf("acceptWithCode: %v", err)
	}
	conn.Close()
	wg.Wait()
}

// TestAcceptWithCodeAllWrong: every connection writes a wrong code.
// acceptWithCode should exhaust its tries and return an error.
func TestAcceptWithCodeAllWrong(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	if tcpL, ok := l.(*net.TCPListener); ok {
		_ = tcpL.SetDeadline(time.Now().Add(5 * time.Second))
	}

	const wrong = "zebra-jasper-meadow"

	// Spam wrong-code connections.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				conn, err := net.Dial("tcp", l.Addr().String())
				if err != nil {
					return
				}
				_ = writePreamble(conn, wrong)
				conn.Close()
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	_, err = acceptWithCode(l, "the-correct-code")
	if err == nil {
		t.Fatalf("expected error when no connection has the right code")
	}
	if !strings.Contains(err.Error(), "no matching connection") {
		t.Errorf("expected 'no matching connection' in error, got: %v", err)
	}
}
