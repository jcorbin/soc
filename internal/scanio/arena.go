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
func (arena *ByteArena) Take() (token ByteArenaToken) {
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

// PruneTo discards any bytes from the arena that aren't reference by a remain token.
func (arena *ByteArena) PruneTo(remain []ByteArenaToken) {
	offset := 0
	for _, token := range remain {
		if token.arena == arena {
			if offset < token.end {
				offset = token.end
			}
		}
	}
	arena.buf = arena.buf[:offset]
	arena.cur = offset
}

// Truncate the token's arena so that it only contains bytes up to, but
// excluding, the receiver token.
// Panics if the token's bytes have already been discarded.
func (token ByteArenaToken) Truncate() {
	token.arena.buf = token.arena.buf[:token.start]
	token.arena.cur = token.start
}

// ByteArenaToken is a handle to a range of bytes written into a ByteArena.
//
// NOTE it may become invalid when the arena is Reset() or when an earlier
// token is Truncate()d
type ByteArenaToken struct {
	byteRange
	arena *ByteArena
}

// Bytes returns a reference to the token bytes within the internal arena buffer.
//
// NOTE this is a slice into the arena's internal buffer, so the caller MUST
// not retain the returned slice, but should copy out of it instead if necessary.
func (token ByteArenaToken) Bytes() []byte {
	if token.arena != nil {
		if buf := token.arena.buf; token.start <= len(buf) && token.end <= len(buf) {
			return buf[token.start:token.end]
		}
	}
	return nil
}

// Text returns a string copy of the token bytes from the internal arena buffer.
func (token ByteArenaToken) Text() string {
	if token.arena != nil {
		if buf := token.arena.buf; token.start <= len(buf) && token.end <= len(buf) {
			return string(buf[token.start:token.end])
		}
	}
	return ""
}

// Empty returns true if the token references no 0 bytes.
func (token ByteArenaToken) Empty() bool {
	return token.end == token.start
}

// Slice returns a sub-token of the receiver, acting similarly to token[i:j].
// Both i and j are token relative, but additionally j may be negative to count
// back from the end of token.
// Panics if the token has no arena (as in the zero value case), or if the
// resulting slice range is invalid.
func (token ByteArenaToken) Slice(i, j int) ByteArenaToken {
	if token.arena == nil {
		panic("cannot slice zero valued token")
	}
	if j < 0 {
		token.end = token.end + 1 + j
	} else {
		token.end = token.start + j
	}
	token.start += i
	if n := len(token.arena.buf); token.end < token.start ||
		token.start < 0 ||
		token.start > n ||
		token.end > n {
		panic(fmt.Sprintf(
			"token slice [%v:%v] out of range [%v:%v] vs %v",
			i, j, token.start, token.end, n))
	}
	return token
}

type byteRange struct{ start, end int }

type ByteTokens struct {
	ByteArena
	ranges []byteRange
}

// Len returns the size of the receiver collection.
func (tokens *ByteTokens) Len() int {
	return len(tokens.ranges)
}

// Get returns the i-th token from the receiver collection.
// Panics if i is out of range.
func (tokens *ByteTokens) Get(i int) (token ByteArenaToken) {
	token.arena = &tokens.ByteArena
	token.byteRange = tokens.ranges[i]
	return token
}

// Set stores the given token into the i-th slot of the receiver collection.
// Panics if i is out of range, or if token is from a foreign arena.
// May be given a zero-valued token to clear an entry.
func (tokens *ByteTokens) Set(i int, token ByteArenaToken) {
	var rng byteRange
	if token.arena != nil {
		if token.arena != &tokens.ByteArena {
			panic("ByteTokens.Set given a token from a foreign arena")
		}
		rng = token.byteRange
	}
	tokens.ranges[i] = rng
}

// Push appends the given token to receiver collection.
// Panics if token is from a foreign arena.
// May be given a zero-valued token to store a placeholder.
func (tokens *ByteTokens) Push(token ByteArenaToken) {
	var rng byteRange
	if token.arena != nil {
		if token.arena != &tokens.ByteArena {
			panic("ByteTokens.Push given a token from a foreign arena")
		}
		rng = token.byteRange
	}
	tokens.ranges = append(tokens.ranges, rng)
}

// Extend appends n empty token ranges into the receiver collection, allowing
// Set(i, token) to work for any i < n.
func (tokens *ByteTokens) Extend(n int) {
	tokens.ranges = append(tokens.ranges, make([]byteRange, n)...)
}

// Truncate discards all tokens upto the i-th from the receiver collection.
// It then discards all unreferenced bytes from the internal arena buffer.
// Panics if i out of range.
func (tokens *ByteTokens) Truncate(i int) {
	remain, offset := tokens.ranges[:i], 0
	for _, token := range remain {
		if token.end > token.start {
			if offset < token.end {
				offset = token.end
			}
		}
	}
	tokens.ranges = remain

	tokens.buf = tokens.buf[:offset]
	tokens.cur = offset
}
