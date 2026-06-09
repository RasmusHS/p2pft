# Peer-to-Peer File Transfer

Peer-to-peer CLI file transfer with end-to-end TLS, resumable transfers, and
a tiny relay that brokers the introduction but never sees the bytes.

Built in Go as Week 8 of a "12 projects in 12 weeks" learning plan. Focus:
TCP/UDP networking, NAT traversal, goroutine-based concurrency, and
streaming integrity verification. Inspired by
[`croc`](https://github.com/schollz/croc) and
[Magic Wormhole](https://github.com/magic-wormhole/magic-wormhole)

## Demo

```text
$ p2pft send report.pdf
Hashing report.pdf (12.3 MiB)... done

  Send code: amber-forest-quartz
  (expires in 10 minutes)

Waiting for receiver to connect...
Receiver connected from 81.66.214.18:48124
Racing 3 candidate(s)... connected via 81.66.214.18:9000
TLS handshake... ok
Sending [##############################] 100% 12.3 MiB / 12.3 MiB
```

```text
$ p2pft receive amber-forest-quartz -o ~/Downloads

  Incoming file: report.pdf (12.3 MiB)
  From: 87.104.246.13:52713
  Save to: /home/me/Downloads/report.pdf
  Listening on: [127.0.0.1:9000 81.66.214.18:9000]

Accept transfer? [y/N]: y
Waiting for sender to connect...
Connected from 87.104.246.13:48128. TLS handshake... ok
Receiving [##############################] 100% 12.3 MiB / 12.3 MiB
Saved to /home/me/Downloads/report.pdf
```

The CLI ships with a default production relay at `wss://relay.rhscloud.com/ws`,
so you can try it without running any infrastructure of your own:

```bash
go install github.com/RasmusHS/p2pft/cmd/p2pft@latest

# Terminal 1
p2pft send /path/to/file

# Terminal 2 — copy the code from the sender's output
p2pft receive <code> -o ~/Downloads
```

## What it actually does

- **Short, memorable codes** generated from a small wordlist (e.g.
  `amber-forest-quartz`). The relay matches sender and receiver by code.
- **Multi-candidate race-dialing.** Receiver advertises every IPv4
  interface it's listening on (loopback, LAN, optional manual
  `--advertise`); sender dials them in parallel and uses whichever wins.
- **Mutual TLS with fingerprint pinning.** Both sides generate ephemeral
  self-signed ed25519 certs at startup. Fingerprints are exchanged through
  the relay; each side rejects any peer cert that doesn't match the
  expected SHA-256. The relay is the only trusted party in the signaling
  phase — and only for introductions, never for bytes.
- **Resumable transfers.** Interrupted transfers can be restarted with the
  same code. The receiver checks any existing `.partial` against the
  expected file hash (via a `.partial.meta` sidecar) and resumes from the
  correct offset.
- **End-to-end integrity.** Sender pre-computes SHA-256 over the full file.
  Receiver computes a rolling SHA-256 over what it actually wrote to disk.
  Sender writes a 32-byte trailer at the end of the stream; receiver
  compares. On mismatch the partial file is kept (for the next resume
  attempt) and the rename to final destination doesn't happen.
- **Production-deployed.** The relay runs on a Hetzner VPS behind Caddy
  with automatic TLS via Let's Encrypt. About €5/month, hardened, runs as
  a sandboxed systemd service.

## How it works

```
┌──────────┐                ┌────────────┐                ┌──────────┐
│  Sender  │                │   Relay    │                │ Receiver │
│          │   WSS/JSON     │  (Hetzner) │   WSS/JSON     │          │
│          ├───signaling────┤            ├───signaling────┤          │
└────┬─────┘                └────────────┘                └────┬─────┘
     │                                                         │
     │   ─── direct mutual-TLS connection (peer to peer) ───   │
     └─────────────────────────────────────────────────────────┘
              framed chunks + 32-byte SHA-256 trailer
```

1. Sender opens a WebSocket to the relay, sends file metadata
   (`filename`, `size`, `sha256`), gets back a session code.
2. Receiver opens a WebSocket to the relay with the code, gets back the
   sender's metadata, prompts the user.
3. Both sides exchange listening-address candidates + TLS cert
   fingerprints through the relay.
4. Receiver's listener accepts incoming connections; sender races dials
   to every advertised candidate. The first successful TCP connection
   wins; the rest are cancelled.
5. Mutual TLS handshake with `VerifyPeerCertificate` callbacks pinning
   each side's fingerprint. Standard CA validation is disabled — the
   fingerprint pin replaces it.
6. File streams over TLS in length-prefixed frames; an end-of-stream
   marker is followed by the 32-byte SHA-256 trailer. Receiver writes to
   `<dest>.partial`, validates the rolling hash matches the trailer,
   atomically renames to the final destination.

### Wire protocol

Signaling (JSON over WebSocket):

| Direction          | Type               | Payload                            |
|--------------------|--------------------|------------------------------------|
| sender → relay     | `sender_hello`     | filename, size, sha256             |
| relay → sender     | `session_created`  | code, expiry                       |
| sender → relay     | `peer_addrs`       | cert fingerprint                   |
| receiver → relay   | `receiver_hello`   | code                               |
| relay → receiver   | `session_found`    | sender's metadata + addrs          |
| receiver → relay   | `peer_addrs`       | listen candidates + fingerprint    |
| relay → sender     | `receiver_joined`  | receiver's addrs                   |
| relay → either     | `session_error`    | (on failure: bad code, expired)    |

Direct peer connection (after race-dial):

```text
[2-byte len][session code]            ← preamble; disambiguates conns
                                        from multi-candidate race
─── TLS handshake (mutual, fingerprint-pinned) ───

[len-prefixed JSON] resume_request    ← receiver → sender
[len-prefixed JSON] transfer_start    ← sender → receiver
[len-prefixed bytes] chunk
[len-prefixed bytes] chunk
...
[length = 0]                          ← end-of-stream marker
[32 bytes]                            ← SHA-256 trailer
```

## Project layout

```text
cmd/
  p2pft/          CLI binary (send, receive subcommands)
  p2pft-relay/    Signaling relay server
internal/
  signaling/     WebSocket client + JSON envelope/message types
  relay/         Relay HTTP/WS handlers + in-memory session store
  transfer/     Wire protocol, framing, streaming hash, resume sidecar
  nat/           Local-address discovery + parallel race-dial
  tlsx/          Ephemeral cert generation + fingerprint pinning
  progress/      Throttled progress bar with byte-rate formatting
```

## Build and test

```bash
git clone https://github.com/RasmusHS/p2pft
cd p2pft
go test ./...          # unit + integration tests across all packages
go build ./cmd/p2pft   # CLI
go build ./cmd/p2pft-relay
```

Tests cover the signaling handshake (with `httptest` driving a real relay),
the framing protocol over `net.Pipe`, the TLS layer (real TCP + mutual
auth, including fingerprint-mismatch detection), the race-dial logic
(winner selection, all-fail, timeout), and the preamble-based connection
pairing that disambiguates same-host races.

## Limitations

Honest assessment of what doesn't work:

- **Both peers behind NAT** with no port forwarding on either side: no
  working direct connection. The "fix" is either TCP simultaneous-open
  hole punching (works for some NAT types, requires `SO_REUSEPORT` and
  coordinated timing) or relay-as-data-fallback (bytes shuttled through
  the relay). Both are real engineering; neither is implemented yet.
- **IPv6** isn't enumerated as a candidate. Dual-stack ordering and
  link-local scopes need careful handling that isn't here.
- **Directory transfer** (the stretch goal from the original brief) isn't
  implemented. Would need a streaming archive format and a protocol
  extension.
- **The relay's session store is in-memory.** Restarting the relay
  strands any in-progress signaling. Tradeoff: a small persistent store
  would help, at the cost of complexity.

## Future work (If I return to this one day)

- TCP simultaneous-open for cooperative-NAT hole punching
- Relay-as-data-fallback when direct connection fails
- Directory transfer (tar + zstd)
- IPv6 candidates with proper happy-eyeballs-style ordering
- A library API alongside the CLI

## Running your own relay

The relay is a single Go binary. The provided systemd unit in this repo
shows a hardened sandbox (`ProtectSystem=strict`, `ProtectHome`,
`NoNewPrivileges`, `PrivateTmp`). My production setup on Hetzner uses:

- Ubuntu 24.04, key-only SSH, UFW + fail2ban
- Caddy 2 reverse-proxying `:443 → 127.0.0.1:8080` with automatic
  Let's Encrypt cert provisioning
- Relay as `User=rasmus` systemd service, binary at `/usr/local/bin/`

CLI clients pick up the default via the `--relay wss://relay.rhscloud.com/ws`
flag (compiled-in default). For local development:

```bash
go run ./cmd/p2pft-relay     # localhost:8080
p2pft send file --relay ws://localhost:8080/ws
```

## Acknowledgements

Heavy intellectual debt to
[Magic Wormhole](https://github.com/magic-wormhole/magic-wormhole) (Python)
and [`croc`](https://github.com/schollz/croc) (Go), which pioneered and
refined the short-code-with-relay pattern. The protocol here is my own
take but the shape of the solution comes from theirs.

## License

[MIT](LICENSE)