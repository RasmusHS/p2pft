package transfer

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Wire format on the direct peer-to-peer connection (after TLS handshake):
//
//   [4-byte length prefix (uint32, big-endian)][chunk bytes]
//   ...
//   [length = 0]   // end-of-stream marker
//   [32 bytes]     // final SHA-256 for verification
//
// JSON messages (ResumeRequest, TransferStart) are exchanged before the
// binary stream starts. They're framed the same way (length-prefixed).

const (
	DefaultChunkSize = 64 * 1024 // 64 KB
	HashSize         = 32        // SHA-256 digest size
	MaxFrameSize     = 1 << 20   // 1 MiB hard cap, rejects malformed peers
)

// ErrFrameTooLarge is returned when a frame's length prefix exceeds MaxFrameSize.
var ErrFrameTooLarge = errors.New("transfer: frame exceeds maximum size")

// WriteFrame writes a length-prefixed chunk to w.
// Passing a zero-length data slice writes the end-of-stream marker.
func WriteFrame(w io.Writer, data []byte) error {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(data)))
	if _, err := w.Write(hdr[:]); err != nil {
		return fmt.Errorf("write frame header: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write frame body: %w", err)
	}
	return nil
}

// ReadFrame reads a length-prefixed chunk from r into buf. Returns the number
// of bytes read. A zero-length frame returns (0, io.EOF) to signal end-of-stream.
//
// If buf is too small for the incoming frame, returns io.ErrShortBuffer
// without consuming the frame body (caller can resize and retry against a new
// reader, but in practice this is a protocol error — kill the connection).
func ReadFrame(r io.Reader, buf []byte) (int, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, fmt.Errorf("read frame header: %w", err)
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 {
		return 0, io.EOF
	}
	if n > MaxFrameSize {
		return 0, ErrFrameTooLarge
	}
	if int(n) > len(buf) {
		return 0, io.ErrShortBuffer
	}
	read, err := io.ReadFull(r, buf[:n])
	if err != nil {
		return read, fmt.Errorf("read frame body: %w", err)
	}
	return read, nil
}
