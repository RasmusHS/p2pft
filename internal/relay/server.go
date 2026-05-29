package relay

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/RasmusHS/p2pft/internal/signaling"
	"github.com/coder/websocket"
)

const (
	// firstMessageTimeout caps how long we wait for the initial hello.
	firstMessageTimeout = 30 * time.Second

	// senderPeerAddrsTimeout caps how long we wait for the sender's PeerAddrs
	// follow-up after SessionCreated.
	senderPeerAddrsTimeout = 30 * time.Second

	// receiverPeerAddrsTimeout caps how long we wait for the receiver's PeerAddrs.
	receiverPeerAddrsTimeout = 30 * time.Second

	// codeCollisionMaxRetries: with a small wordlist, collisions are possible
	// during short bursts; retry a few times before giving up.
	codeCollisionMaxRetries = 5
)

// Server is the signaling relay. It owns the SessionStore and handles
// WebSocket connections from both senders and receivers.
type Server struct {
	sessions *SessionStore
}

func NewServer() *Server {
	return &Server{sessions: NewSessionStore()}
}

// Shutdown stops the session store's reaper.
func (s *Server) Shutdown() {
	s.sessions.Shutdown()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// CLI clients don't send an Origin header; skip browser-style origin checks.
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("ws accept: %v", err)
		return
	}

	client := signaling.FromConn(conn)
	// Default close — handlers may override if they want a specific close reason.
	defer client.Close()

	// Read the first envelope to determine sender vs receiver.
	firstCtx, cancel := context.WithTimeout(r.Context(), firstMessageTimeout)
	defer cancel()

	env, err := client.Read(firstCtx)
	if err != nil {
		log.Printf("relay: read first envelope: %v", err)
		return
	}

	// observedAddr is the source IP:port the relay sees for this WebSocket.
	// In production behind Caddy, this will be 127.0.0.1:xxxxx — the source
	// port is the Caddy→relay hop, not the original NAT mapping. Use
	// X-Forwarded-For only if you trust your proxy chain and document the
	// limitation: UDP hole punching needs a separate UDP probe endpoint
	// because the WS-observed port isn't the UDP NAT mapping anyway.
	observedAddr := r.RemoteAddr

	switch env.Type {
	case signaling.TypeSenderHello:
		var hello signaling.SenderHello
		if err := signaling.DecodePayload(env, &hello); err != nil {
			sendError(r.Context(), client, "invalid sender_hello payload")
			return
		}
		s.handleSender(r.Context(), client, hello, observedAddr)

	case signaling.TypeReceiverHello:
		var hello signaling.ReceiverHello
		if err := signaling.DecodePayload(env, &hello); err != nil {
			sendError(r.Context(), client, "invalid receiver_hello payload")
			return
		}
		s.handleReceiver(r.Context(), client, hello, observedAddr)

	default:
		sendError(r.Context(), client, fmt.Sprintf("unexpected first message type %q", env.Type))
	}
}

func (s *Server) handleSender(
	ctx context.Context,
	client *signaling.Client,
	hello signaling.SenderHello,
	observedAddr string,
) {
	// Generate a code, create a session. Retry on collision.
	var (
		code string
		sess *Session
	)
	for i := 0; i < codeCollisionMaxRetries; i++ {
		c, err := signaling.GenerateCode()
		if err != nil {
			sendError(ctx, client, "failed to generate code")
			return
		}
		created, ok := s.sessions.Create(c, hello)
		if ok {
			code = c
			sess = created
			break
		}
	}
	if sess == nil {
		sendError(ctx, client, "code collision, please retry")
		return
	}

	// Send SessionCreated.
	if err := client.Send(ctx, signaling.TypeSessionCreated, signaling.SessionCreated{
		Code:      code,
		ExpiresIn: int(SessionTTL.Seconds()),
	}); err != nil {
		log.Printf("relay: send SessionCreated: %v", err)
		s.sessions.Claim(code) // remove the orphan
		return
	}

	// Read the sender's PeerAddrs follow-up.
	addrsCtx, cancel := context.WithTimeout(ctx, senderPeerAddrsTimeout)
	var senderAddrs signaling.PeerAddrs
	err := readExpected(addrsCtx, client, signaling.TypePeerAddrs, &senderAddrs)
	cancel()
	if err != nil {
		log.Printf("relay: read sender PeerAddrs: %v", err)
		s.sessions.Claim(code)
		return
	}
	// Overwrite Public with what the relay observed — authoritative.
	senderAddrs.Public = observedAddr
	sess.SenderAddrs = senderAddrs
	close(sess.SenderReady)

	// Park until a receiver claims the code, or we hit the session TTL.
	waitCtx, waitCancel := context.WithTimeout(ctx, SessionTTL)
	defer waitCancel()

	var receiverAddrs signaling.PeerAddrs
	select {
	case receiverAddrs = <-sess.ReceiverJoined:
		// Got one; fall through.
	case <-waitCtx.Done():
		// Either the session expired or the client disconnected.
		// Best-effort: try to remove the session if it's still there.
		s.sessions.Claim(code)
		return
	}

	// Forward to the sender.
	if err := client.Send(ctx, signaling.TypeReceiverJoined, signaling.ReceiverJoined{
		Peer: receiverAddrs,
	}); err != nil {
		log.Printf("relay: send ReceiverJoined: %v", err)
		return
	}
}

func (s *Server) handleReceiver(
	ctx context.Context,
	client *signaling.Client,
	hello signaling.ReceiverHello,
	observedAddr string,
) {
	// Atomic get + remove. Prevents two receivers claiming the same code.
	sess := s.sessions.Claim(hello.Code)
	if sess == nil {
		sendError(ctx, client, "code not found or already claimed")
		return
	}

	// Wait for the sender to have sent its PeerAddrs.
	readyCtx, cancel := context.WithTimeout(ctx, ReceiverWaitForSender)
	select {
	case <-sess.SenderReady:
		// good
	case <-readyCtx.Done():
		cancel()
		sendError(ctx, client, "sender did not provide addresses in time")
		return
	}
	cancel()

	// Send SessionFound with the sender's metadata + addrs.
	if err := client.Send(ctx, signaling.TypeSessionFound, signaling.SessionFound{
		Filename: sess.SenderHello.Filename,
		Size:     sess.SenderHello.Size,
		Sha256:   sess.SenderHello.Sha256,
		Peer:     sess.SenderAddrs,
	}); err != nil {
		log.Printf("relay: send SessionFound: %v", err)
		return
	}

	// Read the receiver's PeerAddrs.
	addrsCtx, addrsCancel := context.WithTimeout(ctx, receiverPeerAddrsTimeout)
	var receiverAddrs signaling.PeerAddrs
	err := readExpected(addrsCtx, client, signaling.TypePeerAddrs, &receiverAddrs)
	addrsCancel()
	if err != nil {
		log.Printf("relay: read receiver PeerAddrs: %v", err)
		return
	}
	receiverAddrs.Public = observedAddr

	// Hand off to the sender's parked goroutine.
	select {
	case sess.ReceiverJoined <- receiverAddrs:
		// delivered
	default:
		// Channel full or sender gone — shouldn't happen with cap=1 unless
		// the sender goroutine already left. Best-effort, drop.
		log.Printf("relay: receiver_joined channel not ready for code %s", sess.Code)
	}
}

// sendError sends a SessionError envelope. Failure to send is logged and ignored —
// the connection is closing anyway.
func sendError(ctx context.Context, client *signaling.Client, reason string) {
	if err := client.Send(ctx, signaling.TypeSessionError, signaling.SessionError{
		Reason: reason,
	}); err != nil {
		log.Printf("relay: send SessionError: %v", err)
	}
}

// readExpected reads the next envelope and asserts its type, decoding into out.
func readExpected(ctx context.Context, client *signaling.Client, expectedType string, out any) error {
	env, err := client.Read(ctx)
	if err != nil {
		return err
	}
	if env.Type != expectedType {
		return fmt.Errorf("expected message type %q, got %q", expectedType, env.Type)
	}
	return signaling.DecodePayload(env, out)
}
