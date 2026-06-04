package main

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	relayURL    string
	outputDir   string
	autoAccept  bool
	listenPort  int
	advertise   []string
	connTimeout int
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
	rootCmd.PersistentFlags().IntVar(&connTimeout, "connect-timeout", 15,
		"Seconds to wait for the direct peer connection before giving up")

	receiveCmd.Flags().StringVarP(&outputDir, "output", "o", ".",
		"Directory to save the received file in")
	receiveCmd.Flags().BoolVarP(&autoAccept, "yes", "y", false,
		"Auto-accept transfers without prompting")
	receiveCmd.Flags().IntVar(&listenPort, "port", 0,
		"Pin the listen port (default 0 = ephemeral). Useful with manual port forwarding.")
	receiveCmd.Flags().StringSliceVar(&advertise, "advertise", nil,
		"Extra host:port to advertise as a connection candidate (repeatable). "+
			"Useful when behind NAT with manual port forwarding: --advertise PUBLIC_IP:FORWARDED_PORT")

	rootCmd.AddCommand(sendCmd, receiveCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
