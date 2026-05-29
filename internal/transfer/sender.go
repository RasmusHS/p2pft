package transfer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
)

// Sender drives the file transfer from the sender's side, once a direct
// peer-to-peer connection has been established.
//
// For step 2 the connection is a plain net.Conn (no TLS). The sender side
// of the protocol is identical with or without TLS, since TLS is just
// transparent wrapping at a lower layer.
type Sender struct {
	// Conn is the direct connection to the peer.
	Conn net.Conn

	// SourcePath is the file to send.
	SourcePath string

	// Size is the total file size in bytes. Must match the SenderHello.Size
	// the receiver got from the relay.
	Size int64

	// Sha256 is the precomputed hex digest of the full source file.
	// Used only as a sanity check (the receiver gets it via SessionFound).
	Sha256 string

	// OnProgress is optional. Called with cumulative bytes sent, after each chunk.
	OnProgress func(written int64)
}

// Run executes the sender side of the transfer protocol.
//
// Wire order:
//  1. Read ResumeRequest (framed JSON).
//  2. Send TransferStart{start_offset} (framed JSON).
//  3. Seek file to start_offset.
//  4. Loop: read chunk from file, WriteFrame to Conn.
//  5. WriteFrame(nil) end-of-stream marker.
//  6. Write 32-byte raw SHA-256 (no framing — fixed size by protocol).
func (s *Sender) Run(ctx context.Context) error {
	// Apply context deadlines to the connection as best we can.
	// net.Conn doesn't take a context, so we wire ctx.Done() to Conn.Close().
	stop := contextCloser(ctx, s.Conn)
	defer stop()

	// 1. Read ResumeRequest from receiver.
	req, err := readJSONFrame[ResumeRequest](s.Conn)
	if err != nil {
		return fmt.Errorf("read resume_request: %w", err)
	}
	if req.Offset < 0 || req.Offset > s.Size {
		return fmt.Errorf("invalid resume offset %d (size=%d)", req.Offset, s.Size)
	}

	// 2. Send TransferStart back.
	if err := writeJSONFrame(s.Conn, TransferStart{StartOffset: req.Offset}); err != nil {
		return fmt.Errorf("write transfer_start: %w", err)
	}

	// 3. Open + seek the file.
	f, err := os.Open(s.SourcePath)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer f.Close()
	if req.Offset > 0 {
		if _, err := f.Seek(req.Offset, io.SeekStart); err != nil {
			return fmt.Errorf("seek to %d: %w", req.Offset, err)
		}
	}

	// 4. Stream chunks. Compute SHA-256 of the entire file in parallel —
	// we already have the precomputed hex digest, so we just write it at
	// the end after the EOS marker.
	buf := make([]byte, DefaultChunkSize)
	var sent int64 = req.Offset
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			if err := WriteFrame(s.Conn, buf[:n]); err != nil {
				return fmt.Errorf("write chunk: %w", err)
			}
			sent += int64(n)
			if s.OnProgress != nil {
				s.OnProgress(sent)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read source: %w", readErr)
		}
	}

	// 5. End-of-stream marker (zero-length frame).
	if err := WriteFrame(s.Conn, nil); err != nil {
		return fmt.Errorf("write EOS: %w", err)
	}

	// 6. Final hash, raw 32 bytes (decoded from our hex digest).
	hashBytes, err := hexDecodeFixed(s.Sha256, HashSize)
	if err != nil {
		return fmt.Errorf("decode sha256: %w", err)
	}
	if _, err := s.Conn.Write(hashBytes); err != nil {
		return fmt.Errorf("write hash: %w", err)
	}

	return nil
}

// contextCloser arranges for c.Close() to be called when ctx is cancelled,
// so blocking reads/writes unblock with a network error. Returns a stop
// function the caller should defer to avoid the watcher goroutine leaking
// past the function's lifetime.
func contextCloser(ctx context.Context, c io.Closer) (stop func()) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = c.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

// writeJSONFrame marshals v and writes it as a single length-prefixed frame.
func writeJSONFrame(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return WriteFrame(w, data)
}

// readJSONFrame reads one length-prefixed frame and unmarshals it into a T.
// A zero-length frame (EOS marker) is treated as an error here — the JSON
// handshake messages should never be zero-length.
func readJSONFrame[T any](r io.Reader) (T, error) {
	var zero T
	buf := make([]byte, MaxFrameSize)
	n, err := ReadFrame(r, buf)
	if err == io.EOF {
		return zero, fmt.Errorf("unexpected EOS during JSON handshake")
	}
	if err != nil {
		return zero, err
	}
	var out T
	if err := json.Unmarshal(buf[:n], &out); err != nil {
		return zero, fmt.Errorf("unmarshal JSON frame: %w", err)
	}
	return out, nil
}

// hexDecodeFixed decodes a hex string and asserts the resulting byte length.
func hexDecodeFixed(s string, want int) ([]byte, error) {
	b, err := hexDecode(s)
	if err != nil {
		return nil, err
	}
	if len(b) != want {
		return nil, fmt.Errorf("expected %d bytes, got %d", want, len(b))
	}
	return b, nil
}
