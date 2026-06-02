package tlsx

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// CertValidity is how long ephemeral session certs are valid for.
// Plenty of headroom for a transfer; short enough that leaked keys age out fast.
const CertValidity = time.Hour

// GenerateCert produces a self-signed TLS certificate + private key suitable
// for an ephemeral peer-to-peer session.
//
// Uses ed25519 (smaller keys, faster signing than RSA). Each side calls this
// once at startup; the fingerprint is exchanged via the relay; the TLS
// handshake then pins each side's cert by fingerprint instead of going
// through normal CA validation.
func GenerateCert() (tls.Certificate, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate key: %w", err)
	}

	// Random 128-bit serial number — required by x509, no semantic meaning here.
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate serial: %w", err)
	}

	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "p2pft-ephemeral"},
		NotBefore:    now.Add(-time.Minute), // tolerate small clock skew
		NotAfter:     now.Add(CertValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		// Each side acts as both server (when accepting) and client (when dialing).
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create certificate: %w", err)
	}

	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("parse certificate: %w", err)
	}

	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  priv,
		Leaf:        parsed,
	}, nil
}

// Fingerprint returns the hex-encoded SHA-256 of the given DER-encoded cert.
// Used inside VerifyPeerFingerprint to compute what the peer just presented.
func Fingerprint(derBytes []byte) string {
	sum := sha256.Sum256(derBytes)
	return hex.EncodeToString(sum[:])
}

// FingerprintCert returns the fingerprint of the leaf cert in a tls.Certificate.
// Convenience wrapper around Fingerprint for the common case.
func FingerprintCert(cert tls.Certificate) string {
	if len(cert.Certificate) == 0 {
		return ""
	}
	return Fingerprint(cert.Certificate[0])
}

// VerifyPeerFingerprint returns a callback suitable for tls.Config.VerifyPeerCertificate.
// It rejects any peer whose presented certificate does not hex-match the expected fingerprint.
//
// Because the certs are self-signed, normal CA validation must be disabled
// (tls.Config.InsecureSkipVerify = true) — the fingerprint pin replaces it.
// An empty expected fingerprint is rejected: never silently accept a wildcard.
func VerifyPeerFingerprint(expected string) func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if expected == "" {
			return errors.New("tlsx: no expected fingerprint configured")
		}
		if len(rawCerts) == 0 {
			return errors.New("tlsx: peer presented no certificate")
		}
		got := Fingerprint(rawCerts[0])
		if got != expected {
			return fmt.Errorf("tlsx: peer certificate fingerprint mismatch: got %s, want %s", got, expected)
		}
		return nil
	}
}

// ServerConfig builds a tls.Config for the side that ACCEPTS the connection
// (in this project, the receiver). The peer's fingerprint must be the one
// received via the relay.
//
// ClientAuth = RequireAnyClientCert means the client MUST present a cert; we
// then verify its fingerprint matches. Without this the client cert would be
// optional and VerifyPeerCertificate could be called with empty rawCerts.
func ServerConfig(cert tls.Certificate, peerFingerprint string) *tls.Config {
	return &tls.Config{
		Certificates:          []tls.Certificate{cert},
		ClientAuth:            tls.RequireAnyClientCert,
		InsecureSkipVerify:    true, // self-signed; fingerprint pin replaces CA validation
		VerifyPeerCertificate: VerifyPeerFingerprint(peerFingerprint),
		MinVersion:            tls.VersionTLS13,
	}
}

// ClientConfig builds a tls.Config for the side that DIALS (in this project,
// the sender). The peer's fingerprint must be the one received via the relay.
func ClientConfig(cert tls.Certificate, peerFingerprint string) *tls.Config {
	return &tls.Config{
		Certificates:          []tls.Certificate{cert},
		InsecureSkipVerify:    true,
		VerifyPeerCertificate: VerifyPeerFingerprint(peerFingerprint),
		MinVersion:            tls.VersionTLS13,
		// ServerName left empty: with InsecureSkipVerify, hostname check is skipped.
	}
}
