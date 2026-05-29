package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var relayURL string

var rootCmd = &cobra.Command{
	Use:   "p2pft",
	Short: "Peer-to-peer file transfer over the internet",
}

var sendCmd = &cobra.Command{
	Use:   "send <file>",
	Short: "Send a file and get a code to share",
	Args:  cobra.ExactArgs(1),
	RunE:  runSend,
}

var receiveCmd = &cobra.Command{
	Use:   "receive <code>",
	Short: "Receive a file using a code",
	Args:  cobra.ExactArgs(1),
	RunE:  runReceive,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&relayURL, "relay",
		"wss://relay.rhscloud.com/ws",
		"Signaling relay URL (use ws://localhost:8080/ws for local dev)")
	rootCmd.AddCommand(sendCmd, receiveCmd)
}

func runSend(cmd *cobra.Command, args []string) error {
	// TODO:
	// 1. Stat + open file, compute SHA-256, get size.
	// 2. Generate ephemeral TLS cert (tlsx.GenerateCert).
	// 3. Dial relay, send SenderHello.
	// 4. Receive SessionCreated, print code to user.
	// 5. Discover local addrs, send PeerAddrs (relay echoes public addr back).
	// 6. Wait for ReceiverJoined.
	// 7. NAT traversal (nat.Punch) → TLS handshake (mutual, fingerprint pinned).
	// 8. Read ResumeRequest, send TransferStart, stream chunks with progress bar.
	return fmt.Errorf("send: not implemented")
}

func runReceive(cmd *cobra.Command, args []string) error {
	// TODO:
	// 1. Generate ephemeral TLS cert.
	// 2. Dial relay, send ReceiverHello{code}.
	// 3. Receive SessionFound (or SessionError).
	// 4. Prompt user: accept filename + size?
	// 5. Discover local addrs, send PeerAddrs (relay forwards as ReceiverJoined).
	// 6. NAT traversal → TLS handshake.
	// 7. Check for partial file, send ResumeRequest.
	// 8. Receive frames into <dest>.partial, update progress + rolling hash.
	// 9. Verify final hash, rename .partial → dest, clean up sidecar.
	return fmt.Errorf("receive: not implemented")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
