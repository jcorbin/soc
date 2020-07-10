package socutil

import (
	"bytes"
	"io"
	"strings"
)

// WriteBuffer combines a byte buffer with a destination writer and flush
// policy. Example use:
//
// 	var buf WriteBuffer
// 	buf.To = os.Stdout
// 	for thing := range things {
// 		fmt.Fprint(&buf, thing)
// 		buf.MaybeFlush() // TODO errcheck
// 	}
// 	buf.Flush() // TODO errcheck
//
// NOTE: the flush methods may be typically deferred when a function scope is available.
type WriteBuffer struct {
	FlushPolicy
	To io.Writer
	bytes.Buffer
}

// FlushPolicy determines when a WriteBuffer should flush during its main write
// phase.
type FlushPolicy interface {
	ShouldFlush(b []byte) int
}

// FlushPolicyFunc is a convenience adaptor for FlushPolicy around a compatible
// anonymous function.
type FlushPolicyFunc func(b []byte) int

// ShouldFlush calls the receiver function pointer.
func (f FlushPolicyFunc) ShouldFlush(b []byte) int { return f(b) }

// Flush attempt to wrtie all of the receiver buffer contents, irregardless of
// the FlushPolicy.
// Should be called after the main write phase.
func (buf *WriteBuffer) Flush() error {
	_, err := buf.WriteTo(buf.To)
	return err
}

// MaybeFlush writes N bytes into To if FlushPolicy returns N > 0.
// The M bytes written are then discarded from the receiver buffer.
// If FlushPolicy is nil, it will be set to FlushLineChunks.
func (buf *WriteBuffer) MaybeFlush() error {
	if buf.FlushPolicy == nil {
		buf.FlushPolicy = FlushPolicyFunc(FlushLineChunks)
	}
	b := buf.Bytes()
	if n := buf.ShouldFlush(b); n > 0 {
		m, err := buf.To.Write(b[:n])
		buf.Next(m)
		return err
	}
	return nil
}

// FlushLineChunks is a FlushPolicy(Func) that flushes as large a chunk as
// possible, through the last written newline byte.
func FlushLineChunks(b []byte) int {
	if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
		return i + 1
	}
	return 0
}

// ErrWriter wraps a writer, tracking it's last error, and preventing futre
// writes after a non-nil.
type ErrWriter struct {
	io.Writer
	Err error
}

// Write passes through to Writer if Err is nil, retaining any returned error.
func (ew *ErrWriter) Write(p []byte) (n int, err error) {
	if ew.Err == nil {
		n, ew.Err = ew.Writer.Write(p)
	}
	return n, ew.Err
}

// PrefixWriter returns a writer that prepends the given string before every
// line written through it.
// The caller SHOULD close it if they care to flush any partial final line.
func PrefixWriter(prefix string, w io.Writer) *Prefixer {
	var p Prefixer
	p.buf.To = w
	p.Prefix = prefix
	return &p
}

// Prefixer supports writing a prefix before every line written to an underling writer.
// Create with PrefixWriter().
// Set Skip true for a one-shot "skip adding the next prefix".
type Prefixer struct {
	Prefix string
	Skip   bool
	buf    WriteBuffer
}

// Close flushes all internally buffered bytes to the underlying writer.
func (p *Prefixer) Close() error { return p.buf.Flush() }

// Flush flushes all internally buffered bytes to the underlying writer.
func (p *Prefixer) Flush() error { return p.buf.Flush() }

// Write writes bytes to the internal buffer, inserting Prefix before every
// line, and then flushes all complete lines to the underlying writer.
func (p *Prefixer) Write(b []byte) (n int, err error) {
	first := true
	for len(b) > 0 {
		if !first {
			p.addPrefix()
		} else if i := p.buf.Len() - 1; i < 0 || p.buf.Bytes()[i] == '\n' {
			p.addPrefix()
			first = false
		} else {
			first = false
		}

		line := b
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			i++
			line = b[:i]
			b = b[i:]
		} else {
			b = nil
		}
		m, _ := p.buf.Write(line)
		n += m
	}
	return n, p.buf.MaybeFlush()
}

// WriteString writes a string to the internal buffer, inserting Prefix before
// every line, and then flushes all complete lines to the underlying writer.
func (p *Prefixer) WriteString(s string) (n int, err error) {
	first := true
	for len(s) > 0 {
		if !first {
			p.addPrefix()
		} else if i := p.buf.Len() - 1; i < 0 || p.buf.Bytes()[i] == '\n' {
			p.addPrefix()
			first = false
		} else {
			first = false
		}

		line := s
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			i++
			line = s[:i]
			s = s[i:]
		} else {
			s = ""
		}
		m, _ := p.buf.WriteString(line)
		n += m
	}
	return n, p.buf.MaybeFlush()
}

func (p *Prefixer) addPrefix() {
	if p.Skip {
		p.Skip = false
	} else {
		p.buf.WriteString(p.Prefix)
	}
}

// WriteLines runs calls the given function around an internal WriteBuffer,
// calling MaybeFlush after every true return, stopping on false return.
// Iteration also stop early if a write error is encountered.
func WriteLines(to io.Writer, next func(w io.Writer, flush func()) bool) error {
	ew, _ := to.(*ErrWriter)
	if ew == nil {
		ew = &ErrWriter{Writer: to}
	}
	var buf WriteBuffer
	buf.To = ew
	for ew.Err == nil && next(&buf, func() { buf.Flush() }) {
		buf.MaybeFlush()
	}
	buf.Flush()
	return ew.Err
}
