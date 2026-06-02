package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/RasmusHS/p2pft/internal/progress"
	"github.com/RasmusHS/p2pft/internal/signaling"
	"github.com/RasmusHS/p2pft/internal/tlsx"
	"github.com/RasmusHS/p2pft/internal/transfer"
)

const acceptTimeout = 30 * time.Second

func runReceive(cmd *cobra.Command, args []string) error {
	code := args[0]

	// 1. Open the listener BEFORE talking to the relay so we already know
	// our listen address when it's time to send PeerAddrs.
	//
	// Step 2: bind to 127.0.0.1 (same-host only). Step 4 will expand this
	// to enumerate interfaces via internal/nat.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer l.Close()
	localAddr := l.Addr().String()

	// 2. Generate an ephemeral TLS cert. Sender will pin its fingerprint.
	cert, err := tlsx.GenerateCert()
	if err != nil {
		return fmt.Errorf("generate tls cert: %w", err)
	}
	fingerprint := tlsx.FingerprintCert(cert)

	// 3. Connect to relay.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, err := signaling.Dial(ctx, relayURL)
	if err != nil {
		return fmt.Errorf("dial relay %s: %w", relayURL, err)
	}
	defer client.Close()

	// 4. Send ReceiverHello{code}.
	if err := client.Send(ctx, signaling.TypeReceiverHello, signaling.ReceiverHello{
		Code: code,
	}); err != nil {
		return fmt.Errorf("send receiver_hello: %w", err)
	}

	// 5. Read SessionFound.
	found, err := readSessionFound(ctx, client)
	if err != nil {
		return err
	}
	if found.Peer.CertFingerprint == "" {
		return fmt.Errorf("sender did not provide a cert fingerprint; aborting")
	}

	// 6. Construct dest path. filepath.Base defends against a malicious
	// sender sending "../../etc/passwd" as a filename.
	filename := filepath.Base(found.Filename)
	dest := filepath.Join(outputDir, filename)

	// 7. Show details, prompt unless --yes.
	fmt.Println()
	fmt.Printf("  Incoming file: %s (%s)\n", filename, progress.FormatBytes(found.Size))
	fmt.Printf("  From: %s\n", found.Peer.Public)
	fmt.Printf("  Save to: %s\n", dest)
	fmt.Println()

	if !autoAccept {
		if !promptYN("Accept transfer?") {
			return fmt.Errorf("transfer declined")
		}
	}

	// 8. Send our PeerAddrs with the listen address and our cert fingerprint.
	if err := client.Send(ctx, signaling.TypePeerAddrs, signaling.PeerAddrs{
		Local:           localAddr,
		CertFingerprint: fingerprint,
	}); err != nil {
		return fmt.Errorf("send peer_addrs: %w", err)
	}

	// Relay's work is done.
	_ = client.Close()

	// 9. Accept the incoming connection with a deadline.
	fmt.Fprintf(os.Stderr, "Waiting for sender on %s...\n", localAddr)
	if tcpL, ok := l.(*net.TCPListener); ok {
		_ = tcpL.SetDeadline(time.Now().Add(acceptTimeout))
	}
	rawConn, err := l.Accept()
	if err != nil {
		return fmt.Errorf("accept: %w", err)
	}
	// Clear the deadline now that we have a conn.
	if tcpL, ok := l.(*net.TCPListener); ok {
		_ = tcpL.SetDeadline(time.Time{})
	}

	// 10. Wrap in TLS. The peer's fingerprint pins their cert.
	tlsConn := tls.Server(rawConn, tlsx.ServerConfig(cert, found.Peer.CertFingerprint))
	defer tlsConn.Close() // closes rawConn too

	fmt.Fprint(os.Stderr, "Connected. TLS handshake... ")
	hsCtx, hsCancel := context.WithTimeout(ctx, tlsHandshakeTimeout)
	if err := tlsConn.HandshakeContext(hsCtx); err != nil {
		hsCancel()
		return fmt.Errorf("tls handshake: %w", err)
	}
	hsCancel()
	fmt.Fprintln(os.Stderr, "ok")

	// 11. Run the transfer over the TLS conn.
	bar := progress.New(found.Size)
	bar.SetLabel("Receiving")

	receiver := &transfer.Receiver{
		Conn:       tlsConn,
		Dest:       dest,
		Size:       found.Size,
		Sha256:     found.Sha256,
		OnProgress: func(n int64) { bar.Set(n) },
	}
	if err := receiver.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr)
		return fmt.Errorf("transfer: %w", err)
	}
	bar.Finish()

	fmt.Fprintf(os.Stderr, "Saved to %s\n", dest)
	return nil
}

func readSessionFound(ctx context.Context, client *signaling.Client) (signaling.SessionFound, error) {
	var out signaling.SessionFound
	env, err := client.Read(ctx)
	if err != nil {
		return out, fmt.Errorf("read session_found: %w", err)
	}
	if env.Type == signaling.TypeSessionError {
		return out, decodeRelayError(env)
	}
	if env.Type != signaling.TypeSessionFound {
		return out, fmt.Errorf("relay sent unexpected message type %q", env.Type)
	}
	if err := signaling.DecodePayload(env, &out); err != nil {
		return out, fmt.Errorf("decode session_found: %w", err)
	}
	return out, nil
}
