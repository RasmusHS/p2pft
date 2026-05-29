package main

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	relayURL   string
	outputDir  string
	autoAccept bool
)

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

	receiveCmd.Flags().StringVarP(&outputDir, "output", "o", ".",
		"Directory to save the received file in")
	receiveCmd.Flags().BoolVarP(&autoAccept, "yes", "y", false,
		"Auto-accept transfers without prompting")

	rootCmd.AddCommand(sendCmd, receiveCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
