package transfer

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
)

// PartialMeta is the sidecar written next to <dest>.partial. We need it
// because a file named "report.pdf.partial" on its own could be from any
// prior transfer of any file that happened to share the name.
type PartialMeta struct {
	Size   int64  `json:"size"`
	Sha256 string `json:"sha256"`
}

// CheckPartial reports how many bytes of a resumable partial exist on disk
// for the given destination + expected total size + expected hash.
//
// Returns (0, nil) if no resumable partial is available. The caller treats
// that as "start from scratch."
func CheckPartial(dest string, expectedSize int64, expectedSha256 string) (int64, error) {
	metaPath := dest + ".partial.meta"
	partialPath := dest + ".partial"

	metaBytes, err := os.ReadFile(metaPath)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	var meta PartialMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		// Corrupt sidecar — treat as no partial; clean up so we don't keep
		// tripping on it.
		_ = os.Remove(metaPath)
		_ = os.Remove(partialPath)
		return 0, nil
	}

	// If sidecar doesn't match this transfer, the partial is for a
	// different file — clean it up and start over.
	if meta.Size != expectedSize || meta.Sha256 != expectedSha256 {
		_ = os.Remove(metaPath)
		_ = os.Remove(partialPath)
		return 0, nil
	}

	stat, err := os.Stat(partialPath)
	if errors.Is(err, os.ErrNotExist) {
		// Sidecar without a partial; clean up sidecar.
		_ = os.Remove(metaPath)
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	// Partial size shouldn't exceed expected size; if it does, the file
	// is corrupt or the transfer already completed. Restart.
	if stat.Size() >= expectedSize {
		_ = os.Remove(metaPath)
		_ = os.Remove(partialPath)
		return 0, nil
	}

	return stat.Size(), nil
}

// WriteMeta writes the sidecar metadata file.
func WriteMeta(dest string, meta PartialMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(dest+".partial.meta", data, 0o644)
}

// CleanupPartial removes both the partial file and its sidecar, if present.
// Called after a successful rename or on certain error paths where we want
// to drop a stale partial.
func CleanupPartial(dest string) {
	_ = os.Remove(dest + ".partial")
	_ = os.Remove(dest + ".partial.meta")
}

// hexDecode is a thin wrapper exposed within the package so sender.go can
// decode the precomputed hex hash without importing encoding/hex directly.
func hexDecode(s string) ([]byte, error) {
	return hex.DecodeString(s)
}
