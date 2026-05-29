package progress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	defaultBarWidth = 30
	// refreshInterval throttles renders. ~20 fps is smooth but cheap.
	refreshInterval = 50 * time.Millisecond
)

// Bar renders a single-line progress bar to stderr (using \r overwrites).
//
// Concurrency: Set, Tick, and Finish are safe to call from multiple goroutines.
// In typical usage there's a single goroutine driving updates (the transfer
// loop), so contention is minimal.
type Bar struct {
	Total int64

	mu      sync.Mutex
	current int64
	label   string
	width   int
	last    time.Time
	out     io.Writer
	done    bool
}

func New(total int64) *Bar {
	return &Bar{
		Total: total,
		width: defaultBarWidth,
		out:   os.Stderr,
	}
}

// SetLabel sets the text prefix shown before the bar.
func (b *Bar) SetLabel(label string) {
	b.mu.Lock()
	b.label = label
	b.mu.Unlock()
}

// Set updates the current count to absolute value c.
func (b *Bar) Set(c int64) {
	b.mu.Lock()
	b.current = c
	shouldRender := !b.done && (time.Since(b.last) >= refreshInterval || c >= b.Total)
	if shouldRender {
		b.last = time.Now()
	}
	b.mu.Unlock()
	if shouldRender {
		b.render()
	}
}

// Tick increments the current count by delta.
func (b *Bar) Tick(delta int64) {
	b.mu.Lock()
	b.current += delta
	c := b.current
	shouldRender := !b.done && (time.Since(b.last) >= refreshInterval || c >= b.Total)
	if shouldRender {
		b.last = time.Now()
	}
	b.mu.Unlock()
	if shouldRender {
		b.render()
	}
}

// Finish renders a final 100% bar and emits a newline. Idempotent.
func (b *Bar) Finish() {
	b.mu.Lock()
	if b.done {
		b.mu.Unlock()
		return
	}
	b.done = true
	b.current = b.Total
	b.mu.Unlock()
	b.render()
	fmt.Fprintln(b.out)
}

func (b *Bar) render() {
	b.mu.Lock()
	cur, tot, label, w := b.current, b.Total, b.label, b.width
	b.mu.Unlock()

	var pct float64
	if tot > 0 {
		pct = float64(cur) / float64(tot)
		if pct > 1 {
			pct = 1
		}
	}
	filled := int(pct * float64(w))
	bar := strings.Repeat("#", filled) + strings.Repeat("-", w-filled)
	// Trailing spaces clear any leftover characters from a previous longer render.
	fmt.Fprintf(b.out, "\r%s [%s] %3.0f%% %s / %s   ",
		label, bar, pct*100,
		FormatBytes(cur), FormatBytes(tot))
}

// Wrap returns a writer that increments the bar as bytes flow through it.
// Useful when the underlying io.Writer doesn't expose a progress callback.
func (b *Bar) Wrap(w io.Writer) io.Writer {
	return &barWriter{b: b, w: w}
}

type barWriter struct {
	b *Bar
	w io.Writer
}

func (bw *barWriter) Write(p []byte) (int, error) {
	n, err := bw.w.Write(p)
	bw.b.Tick(int64(n))
	return n, err
}

// FormatBytes returns a human-readable size like "12.3 MiB" using
// binary (1024-based) units.
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
