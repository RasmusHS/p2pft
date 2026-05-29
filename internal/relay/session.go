package relay

import (
	"context"
	"sync"
	"time"

	"github.com/RasmusHS/p2pft/internal/signaling"
)

const (
	// SessionTTL is the maximum lifetime of a code that hasn't been claimed.
	SessionTTL = 10 * time.Minute

	// ReaperEvery is how often the reaper sweeps expired sessions.
	ReaperEvery = 1 * time.Minute

	// ReceiverWaitForSender is how long a receiver handler waits for the
	// sender to finish sending its PeerAddrs before giving up.
	ReceiverWaitForSender = 10 * time.Second
)

// Session holds the state for one transfer code on the relay.
type Session struct {
	Code        string
	SenderHello signaling.SenderHello
	SenderAddrs signaling.PeerAddrs

	// SenderReady is closed once SenderAddrs has been populated.
	// Receiver handlers wait on this before sending SessionFound.
	SenderReady chan struct{}

	// ReceiverJoined receives the receiver's PeerAddrs once they provide it.
	// Buffered (cap 1) so the receiver's handler doesn't block on send.
	ReceiverJoined chan signaling.PeerAddrs

	CreatedAt time.Time
}

// SessionStore is a thread-safe map of code → *Session with a TTL reaper.
type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]*Session

	ctx    context.Context
	cancel context.CancelFunc
}

func NewSessionStore() *SessionStore {
	ctx, cancel := context.WithCancel(context.Background())
	s := &SessionStore{
		sessions: make(map[string]*Session),
		ctx:      ctx,
		cancel:   cancel,
	}
	go s.reaper()
	return s
}

// Create inserts a new session under code. Returns (nil, false) on collision —
// the caller should generate a fresh code and retry.
func (s *SessionStore) Create(code string, hello signaling.SenderHello) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.sessions[code]; exists {
		return nil, false
	}
	sess := &Session{
		Code:           code,
		SenderHello:    hello,
		SenderReady:    make(chan struct{}),
		ReceiverJoined: make(chan signaling.PeerAddrs, 1),
		CreatedAt:      time.Now(),
	}
	s.sessions[code] = sess
	return sess, true
}

// Claim atomically retrieves and removes a session.
// Returns nil if the code doesn't exist or has been claimed already.
// This prevents two receivers from claiming the same code.
func (s *SessionStore) Claim(code string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[code]
	if !ok {
		return nil
	}
	delete(s.sessions, code)
	return sess
}

// Shutdown stops the reaper goroutine. Safe to call multiple times.
func (s *SessionStore) Shutdown() {
	s.cancel()
}

func (s *SessionStore) reaper() {
	t := time.NewTicker(ReaperEvery)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case now := <-t.C:
			s.mu.Lock()
			for code, sess := range s.sessions {
				if now.Sub(sess.CreatedAt) > SessionTTL {
					delete(s.sessions, code)
				}
			}
			s.mu.Unlock()
		}
	}
}
