package transfer

import (
	"encoding/json"
	"errors"
	"os"
)

// PartialMeta is the sidecar file we write alongside <dest>.partial so we
// can validate that a partial on disk matches the transfer being attempted.
//
// Without this sidecar we'd be guessing — a file named "report.pdf.partial"
// could be left over from any previous transfer of any file that happened
// to share the name.
type PartialMeta struct {
	Size   int64  `json:"size"`
	Sha256 string `json:"sha256"`
}

// CheckPartial reports how many bytes of a resumable partial exist on disk
// for the given destination, given the expected total size and hash.
//
// Returns 0 (no error) if no resumable partial exists. Returns >0 if a
// valid partial exists and the caller should resume from that offset.
//
// TODO: implement. Plan:
//  1. Read <dest>.partial.meta; if missing → return 0.
//  2. Unmarshal PartialMeta; if size/hash don't match expected → return 0
//     (and ideally clean up the stale .partial + .meta).
//  3. Stat <dest>.partial; if missing → return 0.
//  4. If partial size >= expected size → the partial is corrupt or complete;
//     return 0 and let caller decide (probably delete and restart).
//  5. Otherwise return partial size.
func CheckPartial(dest string, expectedSize int64, expectedSha256 string) (int64, error) {
	metaPath := dest + ".partial.meta"
	data, err := os.ReadFile(metaPath)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var meta PartialMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return 0, nil // treat corrupt meta as no partial
	}
	_ = meta
	// TODO: rest of the checks
	return 0, nil
}

// WriteMeta writes the sidecar metadata for <dest>.partial.
// Should be called by the receiver before the first chunk is written.
func WriteMeta(dest string, meta PartialMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(dest+".partial.meta", data, 0o644)
}

// CleanupPartial removes <dest>.partial and <dest>.partial.meta.
// Called after a successful rename to the final destination.
func CleanupPartial(dest string) error {
	// TODO: remove .partial.meta (and .partial if it still exists after rename failure)
	return nil
}
