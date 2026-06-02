package tlsx_test

import (
	"context"
	"crypto/tls"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/RasmusHS/p2pft/internal/tlsx"
)

// TestGenerateCertProducesUniqueCerts verifies the constructor produces
// distinct keys (and therefore distinct fingerprints) on each call.
func TestGenerateCertProducesUniqueCerts(t *testing.T) {
	a, err := tlsx.GenerateCert()
	if err != nil {
		t.Fatalf("first cert: %v", err)
	}
	b, err := tlsx.GenerateCert()
	if err != nil {
		t.Fatalf("second cert: %v", err)
	}

	fpA := tlsx.FingerprintCert(a)
	fpB := tlsx.FingerprintCert(b)

	if fpA == fpB {
		t.Errorf("expected distinct fingerprints, both were %s", fpA)
	}
	if len(fpA) != 64 { // hex SHA-256
		t.Errorf("fingerprint length: want 64, got %d", len(fpA))
	}
}

// TestMutualTLSHandshake verifies two peers with each other's fingerprints
// can establish a mutual-TLS connection and exchange data.
func TestMutualTLSHandshake(t *testing.T) {
	serverCert, err := tlsx.GenerateCert()
	if err != nil {
		t.Fatalf("server cert: %v", err)
	}
	clientCert, err := tlsx.GenerateCert()
	if err != nil {
		t.Fatalf("client cert: %v", err)
	}

	serverFP := tlsx.FingerprintCert(serverCert)
	clientFP := tlsx.FingerprintCert(clientCert)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()

	serverDone := make(chan error, 1)

	// Server side
	go func() {
		tcp, err := l.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		tlsConn := tls.Server(tcp, tlsx.ServerConfig(serverCert, clientFP))
		defer tlsConn.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			serverDone <- err
			return
		}

		// Read 5 bytes, echo back.
		buf := make([]byte, 5)
		if _, err := tlsConn.Read(buf); err != nil {
			serverDone <- err
			return
		}
		if _, err := tlsConn.Write(buf); err != nil {
			serverDone <- err
			return
		}
		serverDone <- nil
	}()

	// Client side
	tcp, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	tlsConn := tls.Client(tcp, tlsx.ClientConfig(clientCert, serverFP))
	defer tlsConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		t.Fatalf("client handshake: %v", err)
	}

	if _, err := tlsConn.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 5)
	if _, err := tlsConn.Read(buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "hello" {
		t.Errorf("echo: want %q, got %q", "hello", buf)
	}

	select {
	case err := <-serverDone:
		if err != nil {
			t.Errorf("server: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("server side timed out")
	}
}

// TestFingerprintMismatch verifies the client rejects a server presenting
// a cert whose fingerprint doesn't match the expected one.
func TestFingerprintMismatch(t *testing.T) {
	serverCert, _ := tlsx.GenerateCert()
	clientCert, _ := tlsx.GenerateCert()
	wrongCert, _ := tlsx.GenerateCert()

	wrongFP := tlsx.FingerprintCert(wrongCert)
	clientFP := tlsx.FingerprintCert(clientCert)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()

	// Server happily presents its real cert; client expects a different fingerprint.
	go func() {
		tcp, err := l.Accept()
		if err != nil {
			return
		}
		tlsConn := tls.Server(tcp, tlsx.ServerConfig(serverCert, clientFP))
		defer tlsConn.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = tlsConn.HandshakeContext(ctx) // expected to fail
	}()

	tcp, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	tlsConn := tls.Client(tcp, tlsx.ClientConfig(clientCert, wrongFP))
	defer tlsConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = tlsConn.HandshakeContext(ctx)
	if err == nil {
		t.Fatalf("expected handshake to fail, but it succeeded")
	}
	if !strings.Contains(err.Error(), "fingerprint mismatch") {
		t.Logf("note: error doesn't mention fingerprint mismatch (Go may wrap it): %v", err)
	}
}

// TestEmptyFingerprintRejected: an empty expected fingerprint must never be
// accepted as a wildcard. If the CLI fails to populate PeerAddrs.CertFingerprint
// (a bug), the handshake should fail rather than silently allow any peer.
func TestEmptyFingerprintRejected(t *testing.T) {
	serverCert, _ := tlsx.GenerateCert()
	clientCert, _ := tlsx.GenerateCert()
	clientFP := tlsx.FingerprintCert(clientCert)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()

	go func() {
		tcp, err := l.Accept()
		if err != nil {
			return
		}
		tlsConn := tls.Server(tcp, tlsx.ServerConfig(serverCert, clientFP))
		defer tlsConn.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = tlsConn.HandshakeContext(ctx) // expected to fail (client gives up)
	}()

	tcp, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	tlsConn := tls.Client(tcp, tlsx.ClientConfig(clientCert, "")) // empty expected FP
	defer tlsConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := tlsConn.HandshakeContext(ctx); err == nil {
		t.Fatalf("expected handshake to fail with empty fingerprint, but it succeeded")
	}
}
