package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/RasmusHS/p2pft/internal/nat"
	"github.com/RasmusHS/p2pft/internal/progress"
	"github.com/RasmusHS/p2pft/internal/signaling"
	"github.com/RasmusHS/p2pft/internal/tlsx"
	"github.com/RasmusHS/p2pft/internal/transfer"
)

const acceptTimeout = 30 * time.Second

func runReceive(cmd *cobra.Command, args []string) error {
	code := args[0]

	// 1. Listen on all interfaces. The OS picks the port unless --port is set.
	listenAddr := net.JoinHostPort("0.0.0.0", strconv.Itoa(listenPort))
	l, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", listenAddr, err)
	}
	defer l.Close()

	// Extract the actual bound port (matters if listenPort was 0).
	_, portStr, _ := net.SplitHostPort(l.Addr().String())
	boundPort, _ := strconv.Atoi(portStr)

	// 2. Build our candidate list: loopback + LAN interfaces + any --advertise extras.
	candidates := []string{net.JoinHostPort("127.0.0.1", portStr)}
	lanAddrs, err := nat.DiscoverLocalAddrs(boundPort)
	if err != nil {
		// Non-fatal — same-host transfers still work via loopback.
		fmt.Fprintf(os.Stderr, "warning: could not discover local addresses: %v\n", err)
	}
	candidates = append(candidates, lanAddrs...)
	for _, a := range advertise {
		candidates = append(candidates, a)
	}

	// 3. Generate TLS cert.
	cert, err := tlsx.GenerateCert()
	if err != nil {
		return fmt.Errorf("generate tls cert: %w", err)
	}
	fingerprint := tlsx.FingerprintCert(cert)

	// 4. Dial relay.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, err := signaling.Dial(ctx, relayURL)
	if err != nil {
		return fmt.Errorf("dial relay %s: %w", relayURL, err)
	}
	defer client.Close()

	// 5. Send ReceiverHello.
	if err := client.Send(ctx, signaling.TypeReceiverHello, signaling.ReceiverHello{
		Code: code,
	}); err != nil {
		return fmt.Errorf("send receiver_hello: %w", err)
	}

	// 6. Read SessionFound.
	found, err := readSessionFound(ctx, client)
	if err != nil {
		return err
	}
	if found.Peer.CertFingerprint == "" {
		return fmt.Errorf("sender did not provide a cert fingerprint; aborting")
	}

	// 7. Construct dest path. filepath.Base defends against malicious filenames.
	filename := filepath.Base(found.Filename)
	dest := filepath.Join(outputDir, filename)

	// 8. Show details and prompt unless --yes.
	fmt.Println()
	fmt.Printf("  Incoming file: %s (%s)\n", filename, progress.FormatBytes(found.Size))
	fmt.Printf("  From: %s\n", found.Peer.Public)
	fmt.Printf("  Save to: %s\n", dest)
	fmt.Printf("  Listening on: %v\n", candidates)
	fmt.Println()

	if !autoAccept {
		if !promptYN("Accept transfer?") {
			return fmt.Errorf("transfer declined")
		}
	}

	// 9. Send our PeerAddrs with the full candidate list.
	if err := client.Send(ctx, signaling.TypePeerAddrs, signaling.PeerAddrs{
		LocalCandidates: candidates,
		CertFingerprint: fingerprint,
	}); err != nil {
		return fmt.Errorf("send peer_addrs: %w", err)
	}

	// Relay's work is done.
	_ = client.Close()

	// 10. Accept the incoming connection with a deadline.
	fmt.Fprintln(os.Stderr, "Waiting for sender to connect...")
	if tcpL, ok := l.(*net.TCPListener); ok {
		_ = tcpL.SetDeadline(time.Now().Add(acceptTimeout))
	}
	rawConn, err := l.Accept()
	if err != nil {
		return fmt.Errorf("accept: %w", err)
	}
	if tcpL, ok := l.(*net.TCPListener); ok {
		_ = tcpL.SetDeadline(time.Time{})
	}

	// 11. TLS-wrap and handshake.
	tlsConn := tls.Server(rawConn, tlsx.ServerConfig(cert, found.Peer.CertFingerprint))
	defer tlsConn.Close()

	fmt.Fprintf(os.Stderr, "Connected from %s. TLS handshake... ", rawConn.RemoteAddr())
	hsCtx, hsCancel := context.WithTimeout(ctx, tlsHandshakeTimeout)
	if err := tlsConn.HandshakeContext(hsCtx); err != nil {
		hsCancel()
		return fmt.Errorf("tls handshake: %w", err)
	}
	hsCancel()
	fmt.Fprintln(os.Stderr, "ok")

	// 12. Run the transfer.
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
