package transfer_test

import (
	"context"
	"crypto/tls"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/RasmusHS/p2pft/internal/tlsx"
	"github.com/RasmusHS/p2pft/internal/transfer"
)

// TestTransferOverTLS runs a real transfer over a real TCP connection wrapped
// in mutual TLS with fingerprint pinning, mirroring what the CLI does at runtime.
//
// We use a real net.Listen here rather than net.Pipe because TLS's larger
// internal writes during handshake interact awkwardly with net.Pipe's
// strictly synchronous semantics.
func TestTransferOverTLS(t *testing.T) {
	const fileSize = 256 * 1024 // straddles several DefaultChunkSize boundaries

	src, srcHash := makeRandomFile(t, fileSize)
	dest := filepath.Join(t.TempDir(), "received.bin")

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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sendErrCh := make(chan error, 1)
	recvErrCh := make(chan error, 1)

	// Receiver (TLS server side, mirrors what runReceive does in the CLI).
	go func() {
		tcp, err := l.Accept()
		if err != nil {
			recvErrCh <- err
			return
		}
		tlsConn := tls.Server(tcp, tlsx.ServerConfig(serverCert, clientFP))
		defer tlsConn.Close()

		if err := tlsConn.HandshakeContext(ctx); err != nil {
			recvErrCh <- err
			return
		}

		receiver := &transfer.Receiver{
			Conn:   tlsConn,
			Dest:   dest,
			Size:   fileSize,
			Sha256: srcHash,
		}
		recvErrCh <- receiver.Run(ctx)
	}()

	// Sender (TLS client side).
	go func() {
		tcp, err := net.Dial("tcp", l.Addr().String())
		if err != nil {
			sendErrCh <- err
			return
		}
		tlsConn := tls.Client(tcp, tlsx.ClientConfig(clientCert, serverFP))
		defer tlsConn.Close()

		if err := tlsConn.HandshakeContext(ctx); err != nil {
			sendErrCh <- err
			return
		}

		sender := &transfer.Sender{
			Conn:       tlsConn,
			SourcePath: src,
			Size:       fileSize,
			Sha256:     srcHash,
		}
		sendErrCh <- sender.Run(ctx)
	}()

	if err := <-sendErrCh; err != nil {
		t.Errorf("sender: %v", err)
	}
	if err := <-recvErrCh; err != nil {
		t.Errorf("receiver: %v", err)
	}

	// Verify dest matches src.
	gotHash := fileSHA256(t, dest)
	if gotHash != srcHash {
		t.Errorf("hash mismatch: src %s, dest %s", srcHash, gotHash)
	}
}
