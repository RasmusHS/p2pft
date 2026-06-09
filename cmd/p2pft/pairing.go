package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

// preambleTimeout bounds how long we wait for the peer's preamble.
// Way more than the actual write needs (~30 bytes), short enough that
// loser connections (which write nothing) get skipped quickly.
const preambleTimeout = 1 * time.Second

// maxAcceptTries is a defensive cap. In practice a handful of losers is
// the worst case — RaceConnect cancels them within ms of picking a winner.
const maxAcceptTries = 10

// maxPreambleLen is a sanity cap on incoming preambles. Codes are ~30 bytes;
// anything larger is malformed or hostile.
const maxPreambleLen = 256

// writePreamble writes a session-identifying string to a freshly-established
// TCP connection, before any other data (and before TLS). The receiver reads
// this to confirm it's the intended connection — not a stray "loser" from
// the sender's multi-candidate race that the accept queue happened to
// surface first.
//
// Wire format: [2-byte big-endian length][N bytes payload]. Plain text.
// The code is already exchanged in plaintext via the relay, so writing it
// here in the clear adds no exposure. TLS protects everything after.
func writePreamble(conn net.Conn, payload string) error {
	if len(payload) > maxPreambleLen {
		return fmt.Errorf("preamble payload too large: %d bytes", len(payload))
	}
	if err := conn.SetWriteDeadline(time.Now().Add(preambleTimeout)); err != nil {
		return err
	}
	defer conn.SetWriteDeadline(time.Time{})

	var hdr [2]byte
	binary.BigEndian.PutUint16(hdr[:], uint16(len(payload)))
	if _, err := conn.Write(hdr[:]); err != nil {
		return fmt.Errorf("write preamble header: %w", err)
	}
	if _, err := conn.Write([]byte(payload)); err != nil {
		return fmt.Errorf("write preamble payload: %w", err)
	}
	return nil
}

// readPreamble reads a length-prefixed session identifier from a connection.
// On error the caller should close the connection — it's either a loser
// being torn down or a hostile/buggy peer.
func readPreamble(conn net.Conn) (string, error) {
	if err := conn.SetReadDeadline(time.Now().Add(preambleTimeout)); err != nil {
		return "", err
	}
	defer conn.SetReadDeadline(time.Time{})

	var hdr [2]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return "", err
	}
	n := binary.BigEndian.Uint16(hdr[:])
	if n > maxPreambleLen {
		return "", fmt.Errorf("preamble payload too large: %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

// acceptWithCode loops over accepted connections until it finds one whose
// preamble matches expectedCode. Non-matching connections — typically
// "losers" from the sender's RaceConnect that the accept queue surfaced
// before the winner — are closed and skipped.
//
// The listener's deadline (set by the caller) bounds the whole operation:
// each Accept call shares the same listener-level deadline, so the total
// time can't exceed it. The returned connection has any per-conn deadlines
// cleared and is ready for TLS handshake.
func acceptWithCode(l net.Listener, expectedCode string) (net.Conn, error) {
	var lastErr error
	for try := 0; try < maxAcceptTries; try++ {
		conn, err := l.Accept()
		if err != nil {
			return nil, fmt.Errorf("accept: %w", err)
		}
		got, err := readPreamble(conn)
		if err != nil {
			conn.Close()
			lastErr = fmt.Errorf("read preamble: %w", err)
			continue
		}
		if got == expectedCode {
			return conn, nil
		}
		conn.Close()
		lastErr = fmt.Errorf("preamble code mismatch: got %q, want %q", got, expectedCode)
	}
	return nil, fmt.Errorf("no matching connection after %d attempts; last error: %w", maxAcceptTries, lastErr)
}
