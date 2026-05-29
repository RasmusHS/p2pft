package relay

import (
	"sync"
	"time"

	"github.com/RasmusHS/p2pft/internal/signaling"
)

const (
	SessionTTL  = 10 * time.Minute
	ReaperEvery = 1 * time.Minute
)

// Session holds the state for one transfer code on the relay.
//
// The sender's handler goroutine populates SenderHello/SenderAddrs and then
// parks on ReceiverJoined. The receiver's handler pushes its addrs onto that
// channel and exits. The sender wakes, forwards the addrs to its peer, and
// exits. After that the session is dead.
type Session struct {
	Code        string
	SenderHello signaling.SenderHello
	SenderAddrs signaling.PeerAddrs

	// Signaled by the receiver's handler once it has reported its addrs.
	// Buffered (cap 1) so the receiver's handler doesn't block.
	ReceiverJoined chan signaling.PeerAddrs

	CreatedAt time.Time
}

// SessionStore is a thread-safe map of code → *Session with a reaper for TTL.
type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

func NewSessionStore() *SessionStore {
	s := &SessionStore{sessions: make(map[string]*Session)}
	go s.reaper()
	return s
}

// Create inserts a new session keyed by code. Returns the session for the
// caller to populate further (e.g. SenderAddrs once known).
//
// TODO: handle collisions (regenerate code, or return error).
func (s *SessionStore) Create(code string, hello signaling.SenderHello) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := &Session{
		Code:           code,
		SenderHello:    hello,
		ReceiverJoined: make(chan signaling.PeerAddrs, 1),
		CreatedAt:      time.Now(),
	}
	s.sessions[code] = sess
	return sess
}

func (s *SessionStore) Get(code string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[code]
	return sess, ok
}

func (s *SessionStore) Delete(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, code)
}

func (s *SessionStore) reaper() {
	t := time.NewTicker(ReaperEvery)
	defer t.Stop()
	for now := range t.C {
		s.mu.Lock()
		for code, sess := range s.sessions {
			if now.Sub(sess.CreatedAt) > SessionTTL {
				delete(s.sessions, code)
			}
		}
		s.mu.Unlock()
	}
}
