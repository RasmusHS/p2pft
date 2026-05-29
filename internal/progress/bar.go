package progress

import "io"

// Bar tracks transfer progress.
//
// Two reasonable paths:
//
//  1. Roll your own: track current/total, render with \r and ANSI on a ticker.
//     Educational, ~50 lines.
//
//  2. Pull in github.com/schollz/progressbar/v3. It's the de facto Go progress
//     bar, handles terminal width detection, eta, throughput, etc.
//
// The scaffold uses option 1 as a stub. Swap in schollz when you're tired of
// looking at it.
type Bar struct {
	Total   int64
	current int64
}

func New(total int64) *Bar {
	return &Bar{Total: total}
}

// Wrap returns a writer that increments the bar as bytes are written through it.
// Useful for the receiver: wrap the .partial file writer with this.
func (b *Bar) Wrap(w io.Writer) io.Writer {
	return &barWriter{b: b, w: w}
}

// Tick increments the bar by n bytes. Useful for the sender side where bytes
// flow into a tls.Conn directly rather than through a writer chain.
func (b *Bar) Tick(n int64) {
	b.current += n
	// TODO: render
}

func (b *Bar) Finish() {
	// TODO: final render + newline
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
