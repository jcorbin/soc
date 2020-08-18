package scanio

import (
	"errors"
	"fmt"
)

var (
	errNoArena   = errors.New("token has no arena")
	errLargeRead = errors.New("token size exceeds arena buffer capacity")
)

type arena struct {
	buf []byte // internal buffer
}

func (ar *arena) load(br byteRange) (rel byteRange, err error) {
	buf := ar.buf
	rel = br

	// service from buffer if possible
	if rel.start >= 0 && rel.end <= len(buf) {
		return rel, nil
	}

	return byteRange{}, fmt.Errorf("cannot load range %v", br)
}

// Token is a handle to a range of bytes under an arena.
type Token struct {
	byteRange
	arena *arena
}

// Bytes returns a reference to the token bytes within the internal arena buffer.
//
// NOTE this is a slice into the arena's internal buffer, so the caller MUST
// not retain the returned slice, but should copy out of it instead if necessary.
func (token Token) Bytes() ([]byte, error) {
	if token.arena == nil {
		return nil, errNoArena
	}
	rel, err := token.arena.load(token.byteRange)
	if err != nil {
		return nil, err
	}
	if rel.len() < token.len() {
		err = errLargeRead
	}
	return token.arena.buf[rel.start:rel.end], err
}

// Text returns a string copy of the token bytes from the internal arena buffer.
func (token Token) Text() (string, error) {
	b, err := token.Bytes()
	return string(b), err
}

// Token formats the token under the fmt.Printf family: the %s and %q verbs
// load the tokens bytes and string print them, or any load error encountered;
// the %v verb works like %s, unless given the + flag as in "%+v", then a debug
// form is printed instead with the start/end offsets, and an arena identifier.
func (token Token) Format(f fmt.State, c rune) {
	switch c {
	case 'q':
		if b, err := token.Bytes(); err != nil {
			fmt.Fprintf(f, "!(ERROR %v)", err)
		} else if prec, ok := f.Precision(); ok {
			fmt.Fprintf(f, "%.*q", prec, b)
		} else {
			fmt.Fprintf(f, "%q", b)
		}

	case 'v':
		if f.Flag('+') {
			fmt.Fprintf(f, "%T(%p)@%v:%v", token.arena, token.arena, token.start, token.end)
			return
		}
		fallthrough
	case 's':
		if b, err := token.Bytes(); err != nil {
			fmt.Fprintf(f, "!(ERROR %v)", err)
		} else if prec, ok := f.Precision(); ok {
			fmt.Fprintf(f, "%.*s", prec, b)
		} else {
			f.Write(b)
		}

	}
}

// Empty returns true if the token references no 0 bytes.
func (token Token) Empty() bool {
	return token.end == token.start
}

// Start returns the token start offset.
func (token Token) Start() int { return token.start }

// End returns the token end offset.
func (token Token) End() int { return token.end }

// End returns the token byte length.
func (token Token) Len() int { return token.len() }

// Slice returns a sub-token of the receiver, acting similarly to token[i:j].
// Both i and j are token relative, but additionally j may be negative to count
// back from the end of token.
// Panics if the token has no arena (as in the zero value case), or if the
// resulting slice range is invalid.
func (token Token) Slice(i, j int) Token {
	if token.arena == nil {
		panic("cannot slice zero valued token")
	}
	old := token.byteRange
	if j < 0 {
		token.end = token.end + 1 + j
	} else {
		token.end = token.start + j
	}
	token.start += i
	if token.end < token.start ||
		token.start < 0 ||
		token.start < old.start ||
		token.start > old.end ||
		token.end > old.end {
		panic(fmt.Sprintf(
			"token slice [%v:%v] out of range [%v:%v]",
			i, j, old.start, old.end))
	}
	return token
}

type byteRange struct{ start, end int }

func (br byteRange) len() int { return br.end - br.start }

// ByteArena implements an io.Writer into an internal in-memory arena, allowing
// token handles to be taken against them.
type ByteArena struct {
	arena
	cur int // write cursor
}

// Write stores p bytes into the internal buffer, returning len(p) and nil error.
func (ba *ByteArena) Write(p []byte) (int, error) {
	ba.buf = append(ba.buf, p...)
	return len(p), nil
}

// WriteString stores s bytes into the internal buffer, returning len(s) and nil error.
func (ba *ByteArena) WriteString(s string) (int, error) {
	ba.buf = append(ba.buf, s...)
	return len(s), nil
}

// Take returns a token referencing any bytes written into the arena since the
// last taken token.
func (ba *ByteArena) Take() (token Token) {
	token.arena = &ba.arena
	token.start = ba.cur
	token.end = len(ba.buf)
	ba.cur = token.end
	return token
}

// Owns returns true only if token refers to the receiver arena.
func (ba *ByteArena) Owns(token Token) bool {
	return token.arena == &ba.arena
}

// Reset discards all bytes from the arena, resetting the internal buffer for reuse.
func (ba *ByteArena) Reset() {
	ba.buf = ba.buf[:0]
	ba.cur = 0
}

// PruneTo discards any bytes from the arena that aren't referenced by any
// remaining token.
// Panics if any of the tokens are foreign.
func (ba *ByteArena) PruneTo(remain []Token) {
	for _, token := range remain {
		if ar := token.arena; ar != nil && ar != &ba.arena {
			panic("ByteArena.PruneTo given a foreign token")
		}
	}
	offset := 0
	for _, token := range remain {
		if offset < token.end {
			offset = token.end
		}
	}
	ba.buf = ba.buf[:offset]
	ba.cur = offset
}

// TruncateTo discards bytes from the receiver arena up to and excluding the given token.
// Panics if the token is foreign or if its bytes have already been discarded.
func (ba *ByteArena) TruncateTo(token Token) {
	if token.arena != &ba.arena {
		panic("ByteArena.TruncateTo given a foreign token")
	}
	ba.buf = ba.buf[:token.start]
	ba.cur = token.start
}
