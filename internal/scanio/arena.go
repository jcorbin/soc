package scanio

import "fmt"

// ByteArena implements an io.Writer that stores bytes in an internal buffer,
// allowing token handles to be taken against them.
type ByteArena struct {
	buf []byte // internal buffer
	cur int    // write cursor
}

// Write stores p bytes into the internal buffer, returning len(p) and nil error.
func (arena *ByteArena) Write(p []byte) (int, error) {
	arena.buf = append(arena.buf, p...)
	return len(p), nil
}

// WriteString stores s bytes into the internal buffer, returning len(s) and nil error.
func (arena *ByteArena) WriteString(s string) (int, error) {
	arena.buf = append(arena.buf, s...)
	return len(s), nil
}

// Take returns a token referencing any bytes written into the arena since the
// last taken token.
func (arena *ByteArena) Take() (token Token) {
	token.arena = arena
	token.start = arena.cur
	token.end = len(arena.buf)
	arena.cur = token.end
	return token
}

// Reset discards all bytes from the arena, resetting the internal buffer for reuse.
func (arena *ByteArena) Reset() {
	arena.buf = arena.buf[:0]
	arena.cur = 0
}

// PruneTo discards any bytes from the arena that aren't reference by any remaining token.
func (arena *ByteArena) PruneTo(remain []Token) {
	for _, token := range remain {
		if ar := token.arena; ar != nil && ar != arena {
			panic("ByteArena.PruneTo given a foreign token")
		}
	}
	offset := 0
	for _, token := range remain {
		if offset < token.end {
			offset = token.end
		}
	}
	arena.buf = arena.buf[:offset]
	arena.cur = offset
}

// TruncateTo discards bytes from the receiver arena up to and excluding the given token.
// Panics if the token is foreign or if its bytes have already been discarded.
func (arena *ByteArena) TruncateTo(token Token) {
	if token.arena != arena {
		panic("ByteArena.TruncateTo given a foreign token")
	}
	arena.buf = arena.buf[:token.start]
	arena.cur = token.start
}

// Token is a handle to a range of bytes under an arena.
type Token struct {
	byteRange
	arena *ByteArena
}

// Bytes returns a reference to the token bytes within the internal arena buffer.
//
// NOTE this is a slice into the arena's internal buffer, so the caller MUST
// not retain the returned slice, but should copy out of it instead if necessary.
func (token Token) Bytes() []byte {
	if token.arena != nil {
		if buf := token.arena.buf; token.start <= len(buf) && token.end <= len(buf) {
			return buf[token.start:token.end]
		}
	}
	return nil
}

// Text returns a string copy of the token bytes from the internal arena buffer.
func (token Token) Text() string {
	if token.arena != nil {
		if buf := token.arena.buf; token.start <= len(buf) && token.end <= len(buf) {
			return string(buf[token.start:token.end])
		}
	}
	return ""
}

// Empty returns true if the token references no 0 bytes.
func (token Token) Empty() bool {
	return token.end == token.start
}

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
