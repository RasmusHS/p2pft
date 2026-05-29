package transfer_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RasmusHS/p2pft/internal/transfer"
)

// TestTransferRoundTrip sends a randomly-generated file from sender to
// receiver over a plain net.Conn pair on localhost, and verifies the
// received file matches byte-for-byte.
func TestTransferRoundTrip(t *testing.T) {
	const fileSize = 750 * 1024 // straddles several DefaultChunkSize boundaries

	src, srcHash := makeRandomFile(t, fileSize)
	dest := filepath.Join(t.TempDir(), "received.bin")

	runTransfer(t, src, srcHash, fileSize, dest)

	// Verify destination matches
	gotHash := fileSHA256(t, dest)
	if gotHash != srcHash {
		t.Fatalf("hash mismatch: src %s, dest %s", srcHash, gotHash)
	}

	// Verify .partial sidecar files are gone
	if _, err := os.Stat(dest + ".partial"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected .partial to be cleaned up, got err=%v", err)
	}
	if _, err := os.Stat(dest + ".partial.meta"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected .partial.meta to be cleaned up, got err=%v", err)
	}
}

// TestTransferProgressCallbacks verifies OnProgress is called and reaches the full size.
func TestTransferProgressCallbacks(t *testing.T) {
	const fileSize = 200 * 1024

	src, srcHash := makeRandomFile(t, fileSize)
	dest := filepath.Join(t.TempDir(), "received.bin")

	sendConn, recvConn := net.Pipe()
	defer sendConn.Close()
	defer recvConn.Close()

	var senderLast, receiverLast int64
	sender := &transfer.Sender{
		Conn:       sendConn,
		SourcePath: src,
		Size:       fileSize,
		Sha256:     srcHash,
		OnProgress: func(n int64) { senderLast = n },
	}
	receiver := &transfer.Receiver{
		Conn:       recvConn,
		Dest:       dest,
		Size:       fileSize,
		Sha256:     srcHash,
		OnProgress: func(n int64) { receiverLast = n },
	}

	runBoth(t, sender, receiver)

	if senderLast != fileSize {
		t.Errorf("sender final progress: want %d, got %d", fileSize, senderLast)
	}
	if receiverLast != fileSize {
		t.Errorf("receiver final progress: want %d, got %d", fileSize, receiverLast)
	}
}

// TestTransferHashMismatch verifies that a tampered file is rejected.
//
// We simulate tampering by having the sender claim a hash that doesn't
// match its actual file contents. The receiver should detect the mismatch
// at the trailer check and refuse the file.
func TestTransferHashMismatch(t *testing.T) {
	const fileSize = 64 * 1024

	src, _ := makeRandomFile(t, fileSize)
	wrongHash := hex.EncodeToString(make([]byte, 32)) // all zeros, very unlikely to match
	dest := filepath.Join(t.TempDir(), "received.bin")

	sendConn, recvConn := net.Pipe()
	defer sendConn.Close()
	defer recvConn.Close()

	sender := &transfer.Sender{
		Conn:       sendConn,
		SourcePath: src,
		Size:       fileSize,
		Sha256:     wrongHash, // sender lies
	}
	receiver := &transfer.Receiver{
		Conn:   recvConn,
		Dest:   dest,
		Size:   fileSize,
		Sha256: wrongHash, // receiver was told the same lie via SessionFound
	}

	sendErr, recvErr := runConcurrent(sender, receiver)
	if recvErr == nil {
		t.Fatalf("receiver: expected hash mismatch error, got nil")
	}
	// Sender succeeds (it just writes whatever it has); the failure is on the receiver.
	_ = sendErr

	// .partial should NOT be cleaned up — we keep it for the next resume attempt
	if _, err := os.Stat(dest + ".partial"); err != nil {
		t.Errorf("expected .partial to be kept on hash mismatch, got err=%v", err)
	}

	// Final destination should not exist
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected dest to not exist on hash mismatch, got err=%v", err)
	}
}

// --- helpers ---

// runTransfer creates a net.Pipe pair, runs sender and receiver concurrently,
// fails the test on any error.
func runTransfer(t *testing.T, srcPath, srcHash string, size int64, dest string) {
	t.Helper()
	sendConn, recvConn := net.Pipe()
	defer sendConn.Close()
	defer recvConn.Close()

	sender := &transfer.Sender{
		Conn:       sendConn,
		SourcePath: srcPath,
		Size:       size,
		Sha256:     srcHash,
	}
	receiver := &transfer.Receiver{
		Conn:   recvConn,
		Dest:   dest,
		Size:   size,
		Sha256: srcHash,
	}
	runBoth(t, sender, receiver)
}

func runBoth(t *testing.T, sender *transfer.Sender, receiver *transfer.Receiver) {
	t.Helper()
	sendErr, recvErr := runConcurrent(sender, receiver)
	if sendErr != nil {
		t.Errorf("sender: %v", sendErr)
	}
	if recvErr != nil {
		t.Errorf("receiver: %v", recvErr)
	}
}

func runConcurrent(sender *transfer.Sender, receiver *transfer.Receiver) (sendErr, recvErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sendCh := make(chan error, 1)
	recvCh := make(chan error, 1)

	go func() {
		sendCh <- sender.Run(ctx)
		// Closing our side unblocks the peer if it's still reading,
		// so a failure on this side doesn't hang the test.
		_ = sender.Conn.Close()
	}()
	go func() {
		recvCh <- receiver.Run(ctx)
		_ = receiver.Conn.Close()
	}()

	sendErr = <-sendCh
	recvErr = <-recvCh
	return
}

// makeRandomFile writes a file of `size` random bytes to a temp dir,
// returns its path and hex SHA-256 digest.
func makeRandomFile(t *testing.T, size int64) (path, hexHash string) {
	t.Helper()
	dir := t.TempDir()
	path = filepath.Join(dir, "source.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	defer f.Close()
	if _, err := io.CopyN(f, rand.Reader, size); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := f.Sync(); err != nil {
		t.Fatalf("sync source: %v", err)
	}
	hexHash, err = transfer.FileSHA256(path)
	if err != nil {
		t.Fatalf("hash source: %v", err)
	}
	return path, hexHash
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	h, err := transfer.FileSHA256(path)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	return h
}
