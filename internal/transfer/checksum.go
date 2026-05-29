package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"os"
)

// FileSHA256 computes the hex-encoded SHA-256 digest of an entire file.
// Called once on the sender side before requesting a code, so the receiver
// can validate the final transfer end-to-end and decide whether a partial
// file on disk is resumable.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// HashingWriter wraps an io.Writer and accumulates a SHA-256 over all bytes
// that pass through it. Use on the receiver side: wrap the .partial file
// writer, write incoming frames through it, then compare Sum() to the
// final hash the sender writes after the end-of-stream marker.
type HashingWriter struct {
	W io.Writer
	h hash.Hash
}

func NewHashingWriter(w io.Writer) *HashingWriter {
	return &HashingWriter{W: w, h: sha256.New()}
}

// NewHashingWriterFromHash wraps w and uses an externally-provided hash.
// Use this when the hash must be seeded with prior bytes (e.g. when
// resuming a partial transfer, the rolling hash needs to cover the bytes
// already on disk before any new bytes arrive).
func NewHashingWriterFromHash(w io.Writer, h hash.Hash) *HashingWriter {
	return &HashingWriter{W: w, h: h}
}

func (hw *HashingWriter) Write(p []byte) (int, error) {
	n, err := hw.W.Write(p)
	if n > 0 {
		hw.h.Write(p[:n])
	}
	return n, err
}

// Sum returns the current SHA-256 digest as raw bytes (32 bytes).
func (hw *HashingWriter) Sum() []byte {
	return hw.h.Sum(nil)
}

// SumHex returns the current SHA-256 digest as a hex string.
func (hw *HashingWriter) SumHex() string {
	return hex.EncodeToString(hw.h.Sum(nil))
}
