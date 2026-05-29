package transfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"os"
)

// Receiver drives the file transfer from the receiver's side.
type Receiver struct {
	// Conn is the direct connection to the peer.
	Conn net.Conn

	// Dest is the final destination path. During transfer the receiver
	// writes to Dest + ".partial" and maintains Dest + ".partial.meta" sidecar.
	Dest string

	// Size is the expected total file size from SessionFound.
	Size int64

	// Sha256 is the expected hex digest from SessionFound.
	Sha256 string

	// OnProgress is optional. Called with cumulative bytes received.
	OnProgress func(written int64)
}

// Run executes the receiver side of the transfer protocol.
//
// Wire order:
//  1. Check for resumable partial, determine start offset.
//  2. Send ResumeRequest (framed JSON).
//  3. Read TransferStart, verify offset matches what we asked for.
//  4. Open .partial in append mode (or O_CREATE if offset=0).
//     If resuming, seed the rolling hash with the existing partial bytes first.
//  5. Loop: ReadFrame from Conn, write through HashingWriter.
//     io.EOF from ReadFrame = end-of-stream marker.
//  6. Read 32-byte trailer hash, compare to running hash.
//  7. On match: rename .partial → Dest, delete sidecar. On mismatch: leave
//     .partial in place for next resume attempt, return error.
func (r *Receiver) Run(ctx context.Context) error {
	stop := contextCloser(ctx, r.Conn)
	defer stop()

	partialPath := r.Dest + ".partial"
	metaPath := r.Dest + ".partial.meta"

	// 1. Determine resume offset.
	startOffset, err := CheckPartial(r.Dest, r.Size, r.Sha256)
	if err != nil {
		return fmt.Errorf("check partial: %w", err)
	}

	// 2. Send ResumeRequest.
	if err := writeJSONFrame(r.Conn, ResumeRequest{Offset: startOffset}); err != nil {
		return fmt.Errorf("write resume_request: %w", err)
	}

	// 3. Read TransferStart.
	start, err := readJSONFrame[TransferStart](r.Conn)
	if err != nil {
		return fmt.Errorf("read transfer_start: %w", err)
	}
	if start.StartOffset != startOffset {
		return fmt.Errorf("sender disagreed on start offset: asked %d, got %d",
			startOffset, start.StartOffset)
	}

	// 4. Open .partial.
	var f *os.File
	if startOffset == 0 {
		f, err = os.Create(partialPath)
	} else {
		f, err = os.OpenFile(partialPath, os.O_WRONLY|os.O_APPEND, 0o644)
	}
	if err != nil {
		return fmt.Errorf("open partial: %w", err)
	}
	defer f.Close()

	// Write/refresh the sidecar metadata.
	if err := WriteMeta(r.Dest, PartialMeta{Size: r.Size, Sha256: r.Sha256}); err != nil {
		return fmt.Errorf("write meta sidecar: %w", err)
	}

	// Seed hash with prior bytes if resuming, so the rolling hash matches
	// what the sender computed over the whole file.
	h := sha256.New()
	if startOffset > 0 {
		if err := seedHashFromPartial(h, partialPath, startOffset); err != nil {
			return fmt.Errorf("seed hash from partial: %w", err)
		}
	}
	hw := NewHashingWriterFromHash(f, h)

	// 5. Stream chunks.
	buf := make([]byte, MaxFrameSize)
	received := startOffset
	for {
		n, err := ReadFrame(r.Conn, buf)
		if errors.Is(err, io.EOF) {
			break // end-of-stream marker
		}
		if err != nil {
			return fmt.Errorf("read frame: %w", err)
		}
		if _, err := hw.Write(buf[:n]); err != nil {
			return fmt.Errorf("write to partial: %w", err)
		}
		received += int64(n)
		if r.OnProgress != nil {
			r.OnProgress(received)
		}
	}

	// 6. Read trailer hash and compare.
	var trailer [HashSize]byte
	if _, err := io.ReadFull(r.Conn, trailer[:]); err != nil {
		return fmt.Errorf("read trailer hash: %w", err)
	}
	got := hw.Sum()
	if !bytes.Equal(trailer[:], got) {
		return fmt.Errorf("hash mismatch: sender %s, receiver %s",
			hex.EncodeToString(trailer[:]), hex.EncodeToString(got))
	}

	// 7. Sync, close, rename, cleanup sidecar.
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync partial: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close partial: %w", err)
	}
	if err := os.Rename(partialPath, r.Dest); err != nil {
		return fmt.Errorf("rename to dest: %w", err)
	}
	_ = os.Remove(metaPath) // best-effort

	return nil
}

// seedHashFromPartial reads up to n bytes from the partial file into h.
// Used when resuming so the rolling hash represents the full file from byte 0.
func seedHashFromPartial(h hash.Hash, path string, n int64) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.CopyN(h, f, n); err != nil {
		return err
	}
	return nil
}
