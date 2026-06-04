package nat_test

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/RasmusHS/p2pft/internal/nat"
)

// TestDiscoverLocalAddrs sanity-checks the structure of returned addresses.
// We can't assert specific values — the host's network configuration varies —
// but everything returned must:
//   - end with the requested port,
//   - parse as a valid IP,
//   - not be loopback or link-local.
func TestDiscoverLocalAddrs(t *testing.T) {
	const port = 54321
	addrs, err := nat.DiscoverLocalAddrs(port)
	if err != nil {
		t.Fatalf("DiscoverLocalAddrs: %v", err)
	}
	// On a CI host with no network, addrs can be empty. That's allowed.
	for _, a := range addrs {
		host, p, err := net.SplitHostPort(a)
		if err != nil {
			t.Errorf("SplitHostPort %q: %v", a, err)
			continue
		}
		if p != "54321" {
			t.Errorf("addr %q: port = %q, want 54321", a, p)
		}
		ip := net.ParseIP(host)
		if ip == nil {
			t.Errorf("addr %q: host %q does not parse as IP", a, host)
			continue
		}
		if ip.IsLoopback() {
			t.Errorf("addr %q is loopback; should have been filtered", a)
		}
		if ip.IsLinkLocalUnicast() {
			t.Errorf("addr %q is link-local; should have been filtered", a)
		}
	}
}

// TestRaceConnectPicksWinner: with two real listeners, RaceConnect should
// succeed and return one of their addresses.
func TestRaceConnectPicksWinner(t *testing.T) {
	l1 := mustListen(t)
	defer l1.Close()
	l2 := mustListen(t)
	defer l2.Close()

	go acceptAndDrop(l1)
	go acceptAndDrop(l2)

	candidates := []string{l1.Addr().String(), l2.Addr().String()}
	conn, addr, err := nat.RaceConnect(context.Background(), candidates, 5*time.Second)
	if err != nil {
		t.Fatalf("RaceConnect: %v", err)
	}
	defer conn.Close()

	if addr != l1.Addr().String() && addr != l2.Addr().String() {
		t.Errorf("winner addr %q is not one of the candidates %v", addr, candidates)
	}
}

// TestRaceConnectAllFail: with candidates that all refuse, RaceConnect
// returns an error mentioning the failures.
func TestRaceConnectAllFail(t *testing.T) {
	// Bind to two ports, then close them so attempts are guaranteed to fail
	// without depending on "well-known closed ports" that vary by OS.
	l1 := mustListen(t)
	addr1 := l1.Addr().String()
	l1.Close()
	l2 := mustListen(t)
	addr2 := l2.Addr().String()
	l2.Close()

	_, _, err := nat.RaceConnect(context.Background(), []string{addr1, addr2}, 500*time.Millisecond)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "all") {
		t.Errorf("error should mention all attempts failed: %v", err)
	}
}

// TestRaceConnectEmpty: empty candidate list is an error, not a panic.
func TestRaceConnectEmpty(t *testing.T) {
	_, _, err := nat.RaceConnect(context.Background(), nil, 1*time.Second)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

// TestRaceConnectMixed: one working candidate among failures; the working
// one should win.
func TestRaceConnectMixed(t *testing.T) {
	good := mustListen(t)
	defer good.Close()
	go acceptAndDrop(good)

	dead := mustListen(t)
	deadAddr := dead.Addr().String()
	dead.Close()

	candidates := []string{deadAddr, good.Addr().String()}
	conn, addr, err := nat.RaceConnect(context.Background(), candidates, 2*time.Second)
	if err != nil {
		t.Fatalf("RaceConnect: %v", err)
	}
	defer conn.Close()
	if addr != good.Addr().String() {
		t.Errorf("winner addr = %q, want %q", addr, good.Addr().String())
	}
}

// --- helpers ---

func mustListen(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return l
}

func acceptAndDrop(l net.Listener) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		_ = c.Close()
	}
}
