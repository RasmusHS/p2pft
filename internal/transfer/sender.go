package transfer

import (
	"context"
	"crypto/tls"
	"io"
)

// Sender drives the file transfer from the sender's side, once a direct
// peer-to-peer connection has been established and TLS has been negotiated.
type Sender struct {
	Conn   *tls.Conn // direct connection to peer
	Source io.ReadSeeker
	Size   int64
	// Sha256 is the precomputed hex digest of the full source file.
	Sha256 string
	// OnProgress is called with bytes written so far. Optional.
	OnProgress func(written int64)
}

// Run executes the sender side:
//  1. Read ResumeRequest (JSON, framed).
//  2. Send TransferStart{start_offset} (JSON, framed).
//  3. Seek Source to start_offset.
//  4. Loop: read chunk from Source, WriteFrame to Conn. Update progress.
//  5. WriteFrame with empty slice (end-of-stream marker).
//  6. Write raw 32-byte SHA-256 to Conn (not length-prefixed — fixed size by protocol).
//
// TODO: implement.
func (s *Sender) Run(ctx context.Context) error {
	_ = ctx
	return nil
}
