package transfer

import (
	"context"
	"crypto/tls"
)

// Receiver drives the file transfer from the receiver's side.
type Receiver struct {
	Conn   *tls.Conn // direct connection to peer
	Dest   string    // final destination path; .partial / .partial.meta sidecar files used during transfer
	Size   int64     // expected total size
	Sha256 string    // expected hex digest, used to validate any existing partial
	// OnProgress is called with bytes written so far. Optional.
	OnProgress func(written int64)
}

// Run executes the receiver side:
//  1. Check for resumable partial (CheckPartial); decide start offset.
//  2. Send ResumeRequest{offset}.
//  3. Read TransferStart{start_offset}, confirm it matches.
//  4. Open <Dest>.partial for append (or O_CREATE if offset=0), wrap in HashingWriter
//     — but seed the hash with the existing partial bytes first if resuming.
//  5. Loop: ReadFrame from Conn, write through HashingWriter, update progress.
//     io.EOF from ReadFrame = end-of-stream marker.
//  6. Read final 32-byte hash from Conn, compare to HashingWriter.Sum().
//  7. On match: rename <Dest>.partial → <Dest>, delete sidecar.
//  8. On mismatch: leave .partial in place for next resume attempt, return error.
//
// TODO: implement.
func (r *Receiver) Run(ctx context.Context) error {
	_ = ctx
	return nil
}
