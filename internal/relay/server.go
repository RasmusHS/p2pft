package relay

import (
	"context"
	"log"
	"net/http"

	"github.com/RasmusHS/p2pft/internal/signaling"
	"github.com/coder/websocket"
)

// Server is the signaling relay. It owns the SessionStore and accepts WebSocket
// connections from both senders and receivers.
type Server struct {
	sessions *SessionStore
}

func NewServer() *Server {
	return &Server{sessions: NewSessionStore()}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("ws accept: %v", err)
		return
	}
	// We don't defer Close here because the per-role handler manages it.

	ctx := r.Context()
	client := &signaling.Client{} // TODO: expose a constructor that wraps an existing conn,
	// or refactor signaling.Client so the relay can use the same Send/Read helpers.
	_ = client

	// Read the first envelope to determine sender vs receiver.
	// TODO:
	// - Read envelope from conn.
	// - Dispatch on env.Type: TypeSenderHello → s.handleSender, TypeReceiverHello → s.handleReceiver.
	// - Anything else → SessionError, close.
	_ = ctx
	_ = conn
}

// handleSender owns the sender's connection for the session lifetime.
//
// TODO:
//  1. Decode SenderHello payload.
//  2. Generate code, create Session in store.
//  3. Send SessionCreated{code, expiresIn} back, including the public addr we
//     observed from conn.RemoteAddr() (consider extending SessionCreated with that
//     field, or send a separate PeerAddrs message back to sender).
//  4. Read PeerAddrs from sender, store on Session.
//  5. Wait on Session.ReceiverJoined channel (or ctx cancel).
//  6. Forward receiver's PeerAddrs to sender as ReceiverJoined.
//  7. Clean up session.
func (s *Server) handleSender(ctx context.Context /* conn, hello */) {
	// TODO
}

// handleReceiver owns the receiver's connection for the session lifetime.
//
// TODO:
// 1. Decode ReceiverHello{code}.
// 2. Look up Session. If missing/expired → SessionError, close.
// 3. Send SessionFound with sender's metadata + addrs + fingerprint.
// 4. Read PeerAddrs from receiver.
// 5. Push receiver's addrs into Session.ReceiverJoined (wakes sender goroutine).
// 6. Close (relay's job is done; peers take it from here).
func (s *Server) handleReceiver(ctx context.Context /* conn, hello */) {
	// TODO
}
