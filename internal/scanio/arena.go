package scanio

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

var (
	errNoArena   = errors.New("token has no arena")
	errNoBacking = errors.New("arena has no backing store")
	errLargeRead = errors.New("token size exceeds arena buffer capacity")
	errClosed    = errors.New("arena closed")
)

// DefaultBufferSize is the default in-memory buffer size to allocate when
// loading under an arena with backing storage, e.g. FileArena.
const DefaultBufferSize = 32 * 1024

type arena struct {
	bufMu  sync.Mutex // lock over buffer movement
	buf    []byte     // internal buffer
	offset int        // start of buf within any back

	back    io.ReaderAt // backing storage
	backErr error       // backing storage error
	size    int64       // size of backing storage
	known   bool        // true if we've checked for size
}

func (ar *arena) setBufSize(bufSize int) {
	if bufSize > 0 {
		ar.buf = make([]byte, 0, bufSize)
	} else {
		ar.buf = nil
	}
}

func (ar *arena) setBack(back io.ReaderAt) {
	ar.back = back
	ar.backErr = nil
	ar.known = false
}

func (ar *arena) load(req byteRange) (rel byteRange, err error) {
	buf := ar.buf
	rel = req.add(-ar.offset) // relativize

	// service from buffer if possible
	if rel.start >= 0 && rel.end <= len(buf) {
		return rel, nil
	}

	// any backing store error
	{
		errBack := ar.backErr
		if errBack == nil && ar.back == nil {
			errBack = errNoBacking
		}
		if errBack != nil {
			return byteRange{}, fmt.Errorf("cannot load range %v: %w", req, errBack)
		}
	}

	// determine reader size if not yet known
	if !ar.known {
		ar.size, _ = readerAtSize(ar.back)
		ar.known = true
	}

	// truncate up to buffer capacity
	n := rel.len()
	if m := cap(buf); n <= m {
		buf = buf[:m]
	} else if m == 0 {
		if n > DefaultBufferSize {
			n = DefaultBufferSize
		}
		buf = make([]byte, DefaultBufferSize)
	} else {
		n = m
	}
	req.end = req.start + n
	ar.buf = buf[:0] // invalid until read succeeds below

	// center any load window slack around the requested range
	load := byteRange{req.start, req.start + len(buf)}
	if h := (len(buf) - n) / 2; h > 0 {
		if h > req.start {
			h = req.start
		}
		load = load.add(-h)
	}

	// but no sense targeting past EOF when we known better
	if sz := int(ar.size); sz != 0 {
		if rem := load.end - sz; rem > 0 {
			load.end -= rem
		}
	}

	// do the read
	buf = buf[:load.len()]
	n, err = ar.back.ReadAt(buf, int64(load.start))
	buf = buf[:n]
	ar.buf, ar.offset = buf, load.start

	// re-relativize and truncate
	if rel = req.add(-ar.offset); rel.start > len(buf) {
		rel = byteRange{len(buf), len(buf)}
		if err == nil {
			err = fmt.Errorf("request range %v not in load range %v", req, load)
		}
	} else if rel.end > len(buf) {
		rel.end = len(buf)
		if err == nil {
			err = io.EOF
		}
	} else if err == io.EOF {
		err = nil // erase EOF error as long as we didn't truncate the request
	}

	return rel, err
}

func readerAtSize(ra io.ReaderAt) (int64, bool) {
	type stater interface{ Stat() (os.FileInfo, error) }
	if st, ok := ra.(stater); ok {
		if info, err := st.Stat(); err == nil {
			return info.Size(), true
		}
	}

	type sizer interface{ Size() int64 }
	if sz, ok := ra.(sizer); ok {
		return sz.Size(), true
	}

	return 0, false
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

	token.arena.bufMu.Lock()
	defer token.arena.bufMu.Unlock()
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

func (br byteRange) add(n int) byteRange {
	br.start += n
	br.end += n
	return br
}

// ByteArena implements an io.Writer into an internal in-memory arena, allowing
// token handles to be taken against them.
type ByteArena struct {
	arena
	cur int // write cursor
}

// Write stores p bytes into the internal buffer, returning len(p) and nil error.
func (ba *ByteArena) Write(p []byte) (int, error) {
	ba.bufMu.Lock()
	defer ba.bufMu.Unlock()
	ba.buf = append(ba.buf, p...)
	return len(p), nil
}

// WriteString stores s bytes into the internal buffer, returning len(s) and nil error.
func (ba *ByteArena) WriteString(s string) (int, error) {
	ba.bufMu.Lock()
	defer ba.bufMu.Unlock()
	ba.buf = append(ba.buf, s...)
	return len(s), nil
}

// Take returns a token referencing any bytes written into the arena since the
// last taken token.
func (ba *ByteArena) Take() (token Token) {
	ba.bufMu.Lock()
	defer ba.bufMu.Unlock()
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
	ba.bufMu.Lock()
	defer ba.bufMu.Unlock()
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
	ba.bufMu.Lock()
	defer ba.bufMu.Unlock()
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
	ba.bufMu.Lock()
	defer ba.bufMu.Unlock()
	ba.buf = ba.buf[:token.start]
	ba.cur = token.start
}

// FileArena is an arena backed by file-like storage in addition to a chunk of
// in-memory buffer. The minimum requirement is a backing io.ReaderAt and
// (presumed fixed) size.
//
// Tokens referents may be constructed against the virtual byte space up to size.
type FileArena struct {
	*arena
}

// Reset (re)initialized the receiver arena to be backed by the given file-like store.
// If size is 0, back must implement a Size() or Stat() method similar to
// strings.Reader or os.File. Any such stat error is returned, preempting arena
// reset.
// The arena is first Close()ed, ignoring any error, and any internal
// buffer space is reused.
// Any extant Tokens become invalid; any attempt to load their contents will
// result in an error.
func (fa *FileArena) Reset(back io.ReaderAt, size int64) error {
	if size == 0 {
		var ok bool
		size, ok = readerAtSize(back)
		if !ok {
			return fmt.Errorf("%T does not implement Size() or Stat(); must specify size", back)
		}
	}

	if fa.arena != nil {
		fa.bufMu.Lock()
		defer fa.bufMu.Unlock()
		_ = fa.doClose() // ignore any close error
	}

	old := fa.arena
	fa.arena = &arena{}
	fa.back = back
	fa.size = size
	fa.known = true
	if old != nil {
		fa.buf = old.buf[:0]
	}

	return nil
}

// Close the FileArena so that future loads return an error.
// If the backing io.ReaderAt implements io.Closer, it is closed.
// Returns any prior backing store error or backing close error.
// It also sets any backing store reference to nil and invalidates any
// in-memory buffer.
func (fa *FileArena) Close() error {
	if fa.arena == nil {
		return nil
	}
	fa.bufMu.Lock()
	defer fa.bufMu.Unlock()
	return fa.doClose()
}

func (fa *FileArena) doClose() error {
	err := fa.backErr
	if err == errClosed {
		err = nil
	}
	if cl, ok := fa.back.(io.Closer); ok {
		if cerr := cl.Close(); err == nil {
			fa.backErr = errClosed
			err = cerr
		}
	} else if fa.backErr == nil {
		fa.backErr = errClosed
	}
	fa.back = nil
	fa.offset = 0
	fa.buf = fa.buf[:0]
	return err
}

// Size returns the, presumably fixed, size of the backing storage
func (fa *FileArena) Size() int64 {
	if fa.arena == nil {
		return 0
	}
	fa.bufMu.Lock()
	defer fa.bufMu.Unlock()
	return fa.size
}

// Ref return a referent token within virtual byte space up to size.
// Its Bytes() will either be service from an in-memory buffer, or read and
// cached for future access.
// The returned token byte range is clipped to the [0, size] interval.
// Returns the zero-Token if start > end.
func (fa *FileArena) Ref(start, end int) (token Token) {
	if fa.arena == nil || start > end {
		return token
	}

	fa.bufMu.Lock()
	defer fa.bufMu.Unlock()
	token.arena = fa.arena
	if start < 0 {
		token.start = 0
	} else {
		token.start = start
	}
	if n := int(fa.size); end > n {
		token.end = n
	} else {
		token.end = end
	}
	return token
}

// Owns returns true only if token refers to the receiver arena.
func (fa *FileArena) Owns(token Token) bool {
	return token.arena == fa.arena
}

// RefN is a convenience for Ref(start, start + n)
func (fa *FileArena) RefN(start, n int) Token {
	return fa.Ref(start, start+n)
}

// RefAll a convenience for Ref(0, Size())
func (fa *FileArena) RefAll() Token {
	return fa.Ref(0, int(fa.size))
}

// ReadAt implements io.ReaderAt that first tries to copy out of arena internal
// memory before falling back to a backing store read ( that will also cache
// back to arena internal memory ).
func (fa *FileArena) ReadAt(p []byte, off int64) (n int, err error) {
	if fa.arena == nil || fa.back == nil {
		return 0, errNoBacking
	}
	fa.bufMu.Lock()
	defer fa.bufMu.Unlock()
	if err := fa.backErr; err != nil {
		return 0, err
	}

	for err == nil && len(p) > 0 {
		var rel byteRange
		if rel, err = fa.load(byteRange{int(off), int(off) + len(p)}); rel.len() > 0 {
			m := copy(p, fa.arena.buf[rel.start:rel.end])
			p = p[n:]
			n += m
			off += int64(m)
		}
	}
	return n, err
}
