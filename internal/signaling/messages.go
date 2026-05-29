package signaling

import (
	"encoding/json"
)

type Envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// First message after WebSocket connect.
type SenderHello struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	Sha256   string `json:"sha256"` // pre-computed for integrity + resume validation
}

// Sent after sender discovers its own addresses.
type PeerAddrs struct {
	Local           string `json:"local"`            // LAN, e.g. "192.168.1.42:54321"
	Public          string `json:"public"`           // public IP:port as seen by relay
	CertFingerprint string `json:"cert_fingerprint"` // SHA-256 of self-signed TLS cert DER
}

type SessionCreated struct {
	Code      string `json:"code"`       // "4-camera-tortoise"
	ExpiresIn int    `json:"expires_in"` // seconds
}

type ReceiverJoined struct {
	Peer PeerAddrs `json:"peer"` // receiver's addrs + cert fingerprint
}

type ReceiverHello struct {
	Code string `json:"code"`
} // Receiver also sends PeerAddrs once it has them.

type SessionFound struct {
	Filename string    `json:"filename"`
	Size     int64     `json:"size"`
	Sha256   string    `json:"sha256"`
	Peer     PeerAddrs `json:"peer"` // sender's addrs + fingerprint
}

type SessionError struct {
	Reason string `json:"reason"` // "code expired", "code not found", "code already claimed"
}
