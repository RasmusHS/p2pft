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

const tlsHandshakeTimeout = 10 * time.Second

func runSend(cmd *cobra.Command, args []string) error {
	sourcePath := args[0]
	filename := filepath.Base(sourcePath)

	// 1. Stat the file. Reject directories — that's a step-6 stretch goal.
	info, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("stat %s: %w", sourcePath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory; directory transfer is not supported yet", sourcePath)
	}
	size := info.Size()

	// 2. Compute SHA-256 up front. Yes, second pass; needed for end-to-end
	// integrity validation and reliable resume.
	fmt.Fprintf(os.Stderr, "Hashing %s (%s)... ", filename, progress.FormatBytes(size))
	hash, err := transfer.FileSHA256(sourcePath)
	if err != nil {
		return fmt.Errorf("hash %s: %w", sourcePath, err)
	}
	fmt.Fprintln(os.Stderr, "done")

	// 3. Generate an ephemeral TLS cert. The fingerprint goes into PeerAddrs;
	// the peer will pin it during the TLS handshake.
	cert, err := tlsx.GenerateCert()
	if err != nil {
		return fmt.Errorf("generate tls cert: %w", err)
	}
	fingerprint := tlsx.FingerprintCert(cert)

	// 4. Connect to relay.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, err := signaling.Dial(ctx, relayURL)
	if err != nil {
		return fmt.Errorf("dial relay %s: %w", relayURL, err)
	}
	defer client.Close()

	// 5. Send SenderHello.
	if err := client.Send(ctx, signaling.TypeSenderHello, signaling.SenderHello{
		Filename: filename,
		Size:     size,
		Sha256:   hash,
	}); err != nil {
		return fmt.Errorf("send sender_hello: %w", err)
	}

	// 6. Receive SessionCreated.
	created, err := readSessionCreated(ctx, client)
	if err != nil {
		return err
	}
	fmt.Println()
	fmt.Printf("  Send code: %s\n", created.Code)
	fmt.Printf("  (expires in %d minutes)\n", created.ExpiresIn/60)
	fmt.Println()

	// 7. Send our PeerAddrs. Sender doesn't listen in step 2, so Local is
	// empty; Public is filled by the relay; CertFingerprint is ours.
	if err := client.Send(ctx, signaling.TypePeerAddrs, signaling.PeerAddrs{
		CertFingerprint: fingerprint,
	}); err != nil {
		return fmt.Errorf("send peer_addrs: %w", err)
	}

	// 8. Wait for ReceiverJoined.
	fmt.Fprintln(os.Stderr, "Waiting for receiver to connect...")
	joined, err := readReceiverJoined(ctx, client)
	if err != nil {
		return err
	}
	if joined.Peer.CertFingerprint == "" {
		return fmt.Errorf("receiver did not provide a cert fingerprint; aborting")
	}
	fmt.Fprintf(os.Stderr, "Receiver connected from %s\n", joined.Peer.Public)

	// Relay's job is done; close the WebSocket.
	_ = client.Close()

	// 9. Dial the receiver's listen addr.
	fmt.Fprintf(os.Stderr, "Dialing %s... ", joined.Peer.Local)
	dialCtx, dialCancel := context.WithTimeout(ctx, 30*time.Second)
	defer dialCancel()
	rawConn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", joined.Peer.Local)
	if err != nil {
		return fmt.Errorf("dial receiver at %s: %w", joined.Peer.Local, err)
	}
	fmt.Fprintln(os.Stderr, "connected")

	// 10. Wrap in TLS. The peer's fingerprint pins their cert.
	tlsConn := tls.Client(rawConn, tlsx.ClientConfig(cert, joined.Peer.CertFingerprint))
	defer tlsConn.Close() // closes rawConn too

	fmt.Fprint(os.Stderr, "TLS handshake... ")
	hsCtx, hsCancel := context.WithTimeout(ctx, tlsHandshakeTimeout)
	if err := tlsConn.HandshakeContext(hsCtx); err != nil {
		hsCancel()
		return fmt.Errorf("tls handshake: %w", err)
	}
	hsCancel()
	fmt.Fprintln(os.Stderr, "ok")

	// 11. Run the transfer over the TLS conn.
	bar := progress.New(size)
	bar.SetLabel("Sending")

	sender := &transfer.Sender{
		Conn:       tlsConn,
		SourcePath: sourcePath,
		Size:       size,
		Sha256:     hash,
		OnProgress: func(n int64) { bar.Set(n) },
	}
	if err := sender.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr)
		return fmt.Errorf("transfer: %w", err)
	}
	bar.Finish()

	return nil
}

func readSessionCreated(ctx context.Context, client *signaling.Client) (signaling.SessionCreated, error) {
	var out signaling.SessionCreated
	env, err := client.Read(ctx)
	if err != nil {
		return out, fmt.Errorf("read session_created: %w", err)
	}
	if env.Type == signaling.TypeSessionError {
		return out, decodeRelayError(env)
	}
	if env.Type != signaling.TypeSessionCreated {
		return out, fmt.Errorf("relay sent unexpected message type %q", env.Type)
	}
	if err := signaling.DecodePayload(env, &out); err != nil {
		return out, fmt.Errorf("decode session_created: %w", err)
	}
	return out, nil
}

func readReceiverJoined(ctx context.Context, client *signaling.Client) (signaling.ReceiverJoined, error) {
	var out signaling.ReceiverJoined
	env, err := client.Read(ctx)
	if err != nil {
		return out, fmt.Errorf("read receiver_joined: %w", err)
	}
	if env.Type == signaling.TypeSessionError {
		return out, decodeRelayError(env)
	}
	if env.Type != signaling.TypeReceiverJoined {
		return out, fmt.Errorf("relay sent unexpected message type %q", env.Type)
	}
	if err := signaling.DecodePayload(env, &out); err != nil {
		return out, fmt.Errorf("decode receiver_joined: %w", err)
	}
	return out, nil
}

func decodeRelayError(env *signaling.Envelope) error {
	var se signaling.SessionError
	if err := signaling.DecodePayload(env, &se); err != nil {
		return fmt.Errorf("relay error (could not decode reason): %w", err)
	}
	return fmt.Errorf("relay: %s", se.Reason)
}
