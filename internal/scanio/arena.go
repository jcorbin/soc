package scanio

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"sync"
)

// CopyTokens writes bytes from all given tokens into dest, returning the
// number of bytes written, and any error that stopped the capy.
func CopyTokens(dest io.Writer, tokens ...Token) (written int64, err error) {
	ranges := make([]byteRange, len(tokens))
	for err == nil && len(tokens) > 0 {
		arena := tokens[0].arena
		ranges = ranges[:0]
		for i := 0; i <= len(tokens); i++ {
			if i == len(tokens) {
				tokens = nil
				break
			}
			token := tokens[i]
			if token.arena != arena {
				tokens = tokens[i:]
				break
			}
			ranges = append(ranges, token.byteRange)
		}
		if arena == nil {
			continue
		}

		// elide empty and coalesce adjacent ranges
		if len(ranges) > 1 {
			ranges = compactRanges(ranges)
		}

		var n int64
		n, err = arena.writeInto(dest, ranges...)
		written += n
	}
	return written, err
}

func compactRanges(ranges []byteRange) []byteRange {
	tmp := ranges
	ranges = ranges[:0]
	cur := tmp[0]
	for _, br := range tmp[1:] {
		if br.empty() {
			continue
		} else if br.start == cur.end {
			cur.end = br.end
		} else {
			if !cur.empty() {
				ranges = append(ranges, cur)
			}
			cur = br
		}
	}
	if !cur.empty() {
		ranges = append(ranges, cur)
	}
	return ranges
}

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

// SetBufSize changes the arena's in-memory buffer size.
// It will either allocate a new buffer and copy any prior contents, or nil-out
// the buffer when passed 0. Any arena load a after such a 0/nil reset
// will allocate a DefaultBufferSize buffer.
func (ar *arena) SetBufSize(bufSize int) {
	if bufSize == 0 {
		ar.buf = nil
	} else if len(ar.buf) > 0 {
		old := ar.buf
		ar.buf = make([]byte, len(old), bufSize)
		copy(ar.buf, old)
	} else {
		ar.buf = make([]byte, 0, bufSize)
	}
}

func (ar *arena) setBack(back io.ReaderAt) {
	ar.back = back
	ar.backErr = nil
	ar.known = false
}

func (ar *arena) writeInto(w io.Writer, brs ...byteRange) (written int64, rerr error) {
	ar.bufMu.Lock()
	defer ar.bufMu.Unlock()

	// no backing store and no ranges: just write buffer
	if ar.back == nil && len(brs) == 0 {
		n, err := w.Write(ar.buf)
		if err == nil && n != len(ar.buf) {
			err = io.ErrShortWrite
		}
		return int64(n), err
	}

	// core logic of the copy loops below
	copyRange := func(br byteRange) (int, error) {
		rel, readErr := ar.load(br)
		if rel.len() == 0 {
			return 0, readErr
		}
		p := ar.buf[rel.start:rel.end]
		n, writeErr := w.Write(p)
		if writeErr == nil && n != len(p) {
			writeErr = io.ErrShortWrite
		}
		written += int64(n)
		if writeErr != nil {
			return n, writeErr
		}
		return n, readErr
	}

	// suppress any EOF readErr about to be returned
	defer func() {
		if rerr == io.EOF {
			rerr = nil
		}
	}()

	// copy loop over all backing store bytes in buf-sized chunks
	if len(brs) == 0 {
		for br := (byteRange{0, cap(ar.buf)}); ; {
			n, err := copyRange(br)
			if err != nil {
				return written, err
			}
			br = br.add(n)
		}
	}

	// copy loop over specific ranges
	for _, br := range brs {
		for br.len() > 0 {
			n, err := copyRange(br)
			if err != nil {
				return written, err
			}
			br.start += n
		}
	}

	return written, nil
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

	default:
		fmt.Fprintf(f, "!(ERROR invalid format verb %%%s)", string(c))
	}
}

// Empty returns true if the token references any bytes in some arena.
func (token Token) Empty() bool { return token.arena == nil || token.empty() }

// Start returns the token start offset.
func (token Token) Start() int { return token.start }

// End returns the token end offset.
func (token Token) End() int { return token.end }

// End returns the token byte length.
func (token Token) Len() int { return token.len() }

// Sub subtracts an other token from the receiver if they overlap.
// If they do not share an arena (an extreme case of no overlap), the receiver
// token is simply returned as head, with an empty tail.
func (token Token) Sub(other Token) (head, tail Token) {
	if other.arena != token.arena {
		return token, Token{}
	}
	bh, bt := token.sub(other.byteRange)
	if !bh.empty() {
		head.arena, head.byteRange = token.arena, bh
	}
	if !bt.empty() {
		tail.arena, tail.byteRange = token.arena, bt
	}
	return head, tail
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

// MakeArea creates a new Area over the geven tokens, which must come from the
// same arena.
func MakeArea(tokens ...Token) (ar Area) {
	for _, token := range tokens {
		ar.Add(token)
	}
	return ar
}

// Area is a set of byte ranges within an arena.
// Token ranges may be added and removed efficiently.
// The area may be written and formatted similarly to a Token.
type Area struct {
	arena  *arena
	ranges []byteRange
}

// WriteTo writes all area byte ranges into the given writer, returning the
// number of bytes written and any write error.
func (ar *Area) WriteTo(dest io.Writer) (n int64, err error) {
	if ar.arena == nil || len(ar.ranges) == 0 {
		return
	}
	return ar.arena.writeInto(dest, ar.ranges...)
}

// AppendTokens appends Token references for each of the area's byte ranges
// into the given slice, returning the new slice value.
func (ar *Area) AppendTokens(into []Token) []Token {
	i := len(into)
	into = append(into, make([]Token, len(ar.ranges))...)
	for j, br := range ar.ranges {
		into[i+j] = Token{br, ar.arena}
	}
	return into
}

// Format write all byte ranges similarly to how their individual Token
// references would be formatted.
// Provides offset information when formatted with %+v.
func (ar Area) Format(f fmt.State, c rune) {
	var quote bool
	switch c {
	case 'v':
		if f.Flag('+') {
			fmt.Fprintf(f, "%T(%p)[", ar.arena, ar.arena)
			for i, br := range ar.ranges {
				if i > 0 {
					f.Write([]byte(" "))
				}
				fmt.Fprintf(f, "@%v:%v", br.start, br.end)
			}
			f.Write([]byte("]"))
			return
		}
	case 's':

	case 'q':
		quote = true

	default:
		fmt.Fprintf(f, "!(ERROR invalid format verb %%%s)", string(c))
	}

	prec, havePrec := f.Precision()
	if quote {
		f.Write([]byte{'"'})
	}

	tok := Token{arena: ar.arena}
	for _, br := range ar.ranges {
		tok.byteRange = br
		b, err := tok.Bytes()
		if err != nil {
			fmt.Fprintf(f, "!(ERROR %v)", err)
			return
		}
		if havePrec && len(b) > prec {
			b = b[:prec]
		}

		var m int
		if quote {
			q := strconv.Quote(string(b)) // TODO wasteful?
			m, err = io.WriteString(f, q[1:len(q)-1])
		} else {
			m, err = f.Write(b)
		}

		if havePrec {
			if prec -= m; prec <= 0 {
				break
			}
		}
		if err != nil {
			return
		}
	}

	if quote {
		f.Write([]byte{'"'})
	}
}

// Add adds a token byte range to the area, potentially merging any prior
// spanned ranges.
// If the Area has not yet had an arena set, it is set to the token's arena.
// Panics if the given token is from a different arena.
func (ar *Area) Add(tok Token) {
	if tok.Empty() {
		return
	}
	if ar.arena == nil {
		ar.arena = tok.arena
	} else if ar.arena != tok.arena {
		panic("Area.Add given a foreign token")
	}

	// TODO worthwhile to eliminate this?
	defer func() {
		ar.ranges = compactRanges(ar.ranges)
	}()
	i := ar.find(tok.start)

	// coalesce spanned ranges
	if j := ar.find(tok.end); j > i {
		n := i + 1
		if j == len(ar.ranges) {
			j--
		}
		ar.ranges[i].end = ar.ranges[j].end
		if k := j + 1; k < len(ar.ranges) {
			n += copy(ar.ranges[n:], ar.ranges[k:])
		}
		ar.ranges = ar.ranges[:n]
	}

	// may extend a prior range
	if i < len(ar.ranges) {
		if prior, overlap := ar.ranges[i].merge(tok.byteRange); overlap {
			ar.ranges[i] = prior
			return
		}
	}

	// otherwise we have a new range to insert
	if i >= len(ar.ranges) {
		ar.ranges = append(ar.ranges, tok.byteRange)
	} else {
		ar.ranges = append(ar.ranges, byteRange{})
		copy(ar.ranges[i+1:], ar.ranges[i:])
		ar.ranges[i] = tok.byteRange
	}
}

// Sub removes a token byte range from the area, potentially fragmenting prior
// touching ranges, and eliding any fully spanned ranges.
// Panics if the given token is from a different arena.
// If the removal results in an empty area, the arena pointer is unset,
// allowing a token from any arena to be added.
func (ar *Area) Sub(tok Token) {
	if tok.Empty() {
		return
	}
	if ar.arena == nil {
		return
	} else if ar.arena != tok.arena {
		panic("Area.Sub given a foreign token")
	}

	defer func() {
		if len(ar.ranges) == 0 {
			ar.arena = nil
		}
	}()

	// TODO worthwhile to eliminate this?
	defer func() {
		ar.ranges = compactRanges(ar.ranges)
	}()

	br := tok.byteRange

	i := ar.find(br.start)
	if i < len(ar.ranges) {
		var tail byteRange
		ar.ranges[i], tail = ar.ranges[i].sub(br)
		if !tail.empty() {
			ar.ranges = append(ar.ranges, byteRange{})
			copy(ar.ranges[i+2:], ar.ranges[i+1:])
			ar.ranges[i+1] = tail
		}
		i++
	}

	j := ar.find(tok.end)
	if j < len(ar.ranges) {
		var head byteRange
		head, ar.ranges[j] = ar.ranges[j].sub(br)
		if !head.empty() {
			ar.ranges = append(ar.ranges, byteRange{})
			copy(ar.ranges[j+1:], ar.ranges[j:])
			ar.ranges[j] = head
		}
		j--
	}

	if j > i {
		n := i
		n += copy(ar.ranges[i:], ar.ranges[j:])
		ar.ranges = ar.ranges[:n]
	}
}

// Empty return true if the area contains no non-empty ranges.
func (ar *Area) Empty() bool {
	for _, br := range ar.ranges {
		if !br.empty() {
			return false
		}
	}
	return true
}

// Clear removes all ranges from the area, and clears any arena pointer.
func (ar *Area) Clear() {
	ar.ranges = ar.ranges[:0]
	ar.arena = nil
}

func (ar *Area) find(offset int) int {
	i := sort.Search(len(ar.ranges), func(i int) bool {
		return ar.ranges[i].start > offset
	})
	if i > 0 && ar.ranges[i-1].end > offset {
		i--
	}
	return i
}

// Find searches the area's ranges for the given offset, returning the number
// of preceding bytes within the area and whether the given offset is
// contained.
//
// NOTE may still return a non-zero before if with false found if the area has
// granges that precede, but none that contain offset.
func (ar Area) Find(offset int) (before int, found bool) {
	i := ar.find(offset)
	found = i < len(ar.ranges)
	for j := 0; j <= i; j++ {
		br := ar.ranges[j]
		end := br.end
		if end > offset {
			end = offset
		}
		before += end - br.start
	}
	return before, found
}

type byteRange struct{ start, end int }

func (br byteRange) empty() bool { return br.end == br.start }
func (br byteRange) len() int    { return br.end - br.start }

func (br byteRange) contains(offset int) bool {
	return offset >= br.start && offset < br.end
}

func (br byteRange) merge(other byteRange) (_ byteRange, overlap bool) {
	if br.contains(other.start) {
		if br.end < other.end {
			br.end = other.end
		}
		overlap = true
	}
	if br.contains(other.end) {
		if br.start > other.start {
			br.start = other.start
		}
		overlap = true
	}
	return br, overlap
}

func (br byteRange) add(n int) byteRange {
	br.start += n
	br.end += n
	return br
}

func (br byteRange) sub(other byteRange) (head, tail byteRange) {
	head = br
	if other.start < br.end {
		if other.end < br.start {
			return
		}
		head.end = other.start
		if head.end < head.start {
			head = byteRange{}
		}
		if other.end < br.end {
			tail = br
			tail.start = other.end
		}
	}
	return
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
	if size == 0 && back != nil {
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
