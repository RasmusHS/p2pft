package tlsx

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
)

// GenerateCert produces a self-signed TLS certificate + private key suitable
// for an ephemeral peer-to-peer session.
//
// TODO: implement. Plan:
//  1. Generate an ed25519 key (crypto/ed25519.GenerateKey).
//  2. Build an x509.Certificate template:
//     - SerialNumber: random 128-bit
//     - NotBefore: now - 1min, NotAfter: now + 1h (sessions are short)
//     - Subject: arbitrary (e.g. "p2pft-ephemeral")
//     - KeyUsage: KeyUsageDigitalSignature
//     - ExtKeyUsage: ServerAuth + ClientAuth (we need both — mutual TLS)
//  3. x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv).
//  4. Wrap into tls.Certificate{Certificate: [][]byte{derBytes}, PrivateKey: priv}.
func GenerateCert() (tls.Certificate, error) {
	return tls.Certificate{}, errors.New("not implemented")
}

// Fingerprint returns the hex-encoded SHA-256 of the cert's DER encoding.
// This is what the relay forwards between peers; each side pins the other's
// fingerprint during TLS handshake.
func Fingerprint(derBytes []byte) string {
	sum := sha256.Sum256(derBytes)
	return hex.EncodeToString(sum[:])
}

// VerifyPeerFingerprint returns a callback suitable for tls.Config.VerifyPeerCertificate.
// It rejects any peer whose presented certificate does not hex-match the expected fingerprint.
//
// Because the certs are self-signed, normal CA validation must be disabled
// (tls.Config.InsecureSkipVerify = true) — the fingerprint pin replaces it.
func VerifyPeerFingerprint(expected string) func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return errors.New("tlsx: peer presented no certificate")
		}
		got := Fingerprint(rawCerts[0])
		if got != expected {
			return errors.New("tlsx: peer certificate fingerprint mismatch")
		}
		return nil
	}
}
