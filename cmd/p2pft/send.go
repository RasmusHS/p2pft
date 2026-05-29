package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/RasmusHS/p2pft/internal/progress"
	"github.com/RasmusHS/p2pft/internal/signaling"
	"github.com/RasmusHS/p2pft/internal/transfer"
)

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

	// 2. Compute SHA-256 up front. Yes, this is a second pass over the file
	// (the transfer is the first), and yes, for big files there's a perceptible
	// delay before the code appears. That's the trade-off for end-to-end
	// integrity validation and reliable resume.
	fmt.Fprintf(os.Stderr, "Hashing %s (%s)... ", filename, progress.FormatBytes(size))
	hash, err := transfer.FileSHA256(sourcePath)
	if err != nil {
		return fmt.Errorf("hash %s: %w", sourcePath, err)
	}
	fmt.Fprintln(os.Stderr, "done")

	// 3. Connect to relay.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, err := signaling.Dial(ctx, relayURL)
	if err != nil {
		return fmt.Errorf("dial relay %s: %w", relayURL, err)
	}
	defer client.Close()

	// 4. Send SenderHello.
	if err := client.Send(ctx, signaling.TypeSenderHello, signaling.SenderHello{
		Filename: filename,
		Size:     size,
		Sha256:   hash,
	}); err != nil {
		return fmt.Errorf("send sender_hello: %w", err)
	}

	// 5. Receive SessionCreated.
	created, err := readSessionCreated(ctx, client)
	if err != nil {
		return err
	}

	// The code is the user-facing artifact — print it prominently on stdout
	// so it's easy to copy from terminals and survives stderr redirection.
	fmt.Println()
	fmt.Printf("  Send code: %s\n", created.Code)
	fmt.Printf("  (expires in %d minutes)\n", created.ExpiresIn/60)
	fmt.Println()

	// 6. Send our PeerAddrs. In step 2 the sender doesn't listen, so Local is
	// empty. Public will be filled in by the relay. CertFingerprint comes
	// later with TLS (step 3).
	if err := client.Send(ctx, signaling.TypePeerAddrs, signaling.PeerAddrs{}); err != nil {
		return fmt.Errorf("send peer_addrs: %w", err)
	}

	// 7. Wait for ReceiverJoined. This can take up to SessionTTL (10 min).
	fmt.Fprintln(os.Stderr, "Waiting for receiver to connect...")
	joined, err := readReceiverJoined(ctx, client)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Receiver connected from %s\n", joined.Peer.Public)

	// Relay's job is done; close the WebSocket to free its resources.
	_ = client.Close()

	// 8. Dial the receiver's listen addr.
	fmt.Fprintf(os.Stderr, "Dialing %s... ", joined.Peer.Local)
	dialCtx, dialCancel := context.WithTimeout(ctx, 30*time.Second)
	defer dialCancel()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", joined.Peer.Local)
	if err != nil {
		return fmt.Errorf("dial receiver at %s: %w", joined.Peer.Local, err)
	}
	defer conn.Close()
	fmt.Fprintln(os.Stderr, "connected")

	// 9. Run the transfer with a progress bar.
	bar := progress.New(size)
	bar.SetLabel("Sending")

	sender := &transfer.Sender{
		Conn:       conn,
		SourcePath: sourcePath,
		Size:       size,
		Sha256:     hash,
		OnProgress: func(n int64) { bar.Set(n) },
	}
	if err := sender.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr) // ensure error appears on a fresh line below the bar
		return fmt.Errorf("transfer: %w", err)
	}
	bar.Finish()

	return nil
}

// readSessionCreated reads the next envelope and asserts it's a SessionCreated
// (or surfaces a SessionError with a useful message).
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
