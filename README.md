# Peer-to-Peer File Transfer

Peer-to-peer CLI file transfer. One side runs `send <file>`, gets a code.
The other runs `receive <code>`. A small relay handles signaling; files
transfer directly between peers when NAT traversal succeeds.

Week 8 project of a 12-projects-in-12-weeks plan. Focus: Go networking,
NAT traversal, TLS, resume, checksumming.

## Repo layout

```
cmd/
  p2pft/         CLI binary (send, receive subcommands)
  p2pft-relay/   Signaling relay server
internal/
  signaling/     WebSocket client + envelope/message types
  relay/         Relay HTTP/WS handlers + session store
  transfer/      Peer-to-peer wire protocol, framing, resume, checksum
  nat/           Local address discovery + hole punching
  tlsx/          Ephemeral cert generation + fingerprint pinning
  progress/      Transfer progress bar
```

## Dev quickstart

```bash
# Initialize (do once; replace 'yourusername' with your GitHub handle
# everywhere in the codebase, including internal imports)
go mod init github.com/yourusername/p2pft
go get github.com/coder/websocket
go get github.com/spf13/cobra
go mod tidy

# Build both binaries
make build

# Run the relay locally
make run-relay

# In a second terminal, send a file via the local relay
./bin/p2pft send testdata/sample.txt --relay ws://localhost:8080/ws

# In a third terminal, receive
./bin/p2pft receive <code> --relay ws://localhost:8080/ws
```

## Production relay

The relay is deployed to a Hetzner CX22 at `wss://relay.rhscloud.com/ws`
(replace as appropriate). The CLI defaults to that URL; override with
`--relay` for local development.