package scanio

import (
	"fmt"
	"io"
)

// Editor supports editing tokenized arena content.
// It manages a slice of Tokens, an internal ByteArena for easily adding new
// content tokens, and cursor references within content space.
type Editor struct {
	arena   ByteArena    // contains newly written content
	content []Token      // may reference other arenas
	cursors []cursorData // active within content byte space
	tmp     []Token      // scratch space
}

type cursorData struct {
	loc   int
	conti int
	off   int
}

// Cursor is a handle to a point within an Editor's Content space.
type Cursor struct {
	*Editor
	id int
}

// Append a content token.
func (ed *Editor) Append(tok Token) {
	ed.content = append(ed.content, tok)
}

// Write stores p into the editor arena, and then append a Content token
// referencing it.
func (ed *Editor) Write(p []byte) (int, error) {
	n, err := ed.arena.Write(p)
	ed.content = append(ed.content, ed.arena.Take())
	return n, err
}

// WriteString stores s into the editor arena, and then append a Content token
// referencing it.
func (ed *Editor) WriteString(s string) (int, error) {
	n, err := ed.arena.WriteString(s)
	ed.content = append(ed.content, ed.arena.Take())
	return n, err
}

// WriteTo writes all Content token contents into the given writer, returning
// the number of bytes written and any write error.
func (ed *Editor) WriteTo(w io.Writer) (n int64, err error) {
	return CopyTokens(w, ed.content...)
}

// CursorAt creates a new cursor within the editor referencing a given Content
// location, and return a handle for using it.
// If loc exceed Content length, it is truncated and the returned cursor points
// just past the last byte.
func (ed *Editor) CursorAt(loc int) Cursor {
	var dat cursorData
	for off := loc; dat.conti < len(ed.content); {
		tok := ed.content[dat.conti]
		if n := tok.Len(); off < n {
			dat.off = off
			dat.loc += off
			break
		} else {
			dat.conti++
			dat.loc += n
			off -= n
		}
	}

	id := 0
	for id < len(ed.cursors) {
		if ed.cursors[id].loc < 0 {
			ed.cursors[id] = dat
		}
		id++
	}
	if id >= len(ed.cursors) {
		ed.cursors = append(ed.cursors, dat)
	}
	return Cursor{ed, id}
}

// Close the receiver cursor, freeing its data within its editor.
// The cursor handle becomes invalid for use after this call, and must not be
// retained.
func (edc *Cursor) Close() error {
	if edc.Editor != nil {
		edc.cursors[edc.id] = cursorData{-1, -1, -1}
		edc.Editor = nil
		edc.id = -1
	}
	return nil
}

// Copy the cursor, returning a handle to the clone.
func (edc Cursor) Copy() Cursor { return edc.CursorAt(edc.Location()) }

// Location returns the cursor's offset withing editor Content space.
func (edc Cursor) Location() int { return edc.cursors[edc.id].loc }

// To moves the cursor to the given location, truncated to the [0, Size] interval.
func (edc Cursor) To(loc int) { edc.By(edc.Location() - loc) }

// By moves the cursor by a relative offset foreward or backward, truncated to the [0, Size] interval.
func (edc Cursor) By(off int) { edc.moveCursor(edc.id, off) }

func (ed *Editor) moveCursor(i, off int) {
	var tok Token
	cur := &ed.cursors[i]
	if cur.conti < len(ed.content) {
		tok = ed.content[cur.conti]
	}
	for {
		if off > 0 {
			if tok.arena == nil {
				return // cannot go past EOC
			}

			// forward through current token
			n := tok.Len()
			fore := n - cur.off
			if fore > off {
				fore = off
			}
			cur.off += fore
			cur.loc += fore

			// next token
			if cur.off == n {
				cur.off = 0
				if cur.conti++; cur.conti >= len(ed.content) {
					return // hit EOC
				}
				tok = ed.content[cur.conti]
			}
			off -= fore
			continue
		}

		if off < 0 {
			// backward through current token
			back := cur.off
			// XXX back++
			if back > -off {
				back = -off
			} else if back == 0 {
				back = 1
			}
			cur.off -= back
			cur.loc -= back

			// previous token
			if o := cur.off; o < 0 {
				if cur.conti--; cur.conti < 0 {
					cur.off = 0
					cur.loc = 0
					return // hit SOC
				}
				tok = ed.content[cur.conti]
				cur.off = tok.Len() + o
				off -= o
			} else {
				off += back
			}
			continue
		}

		// off == 0
		return // done
	}
}

// Insert adds the given token(s) at the cursor's position, splitting any token
// that it is within.
func (edc Cursor) Insert(tokens ...Token) {
	at := edc.cursors[edc.id]

	content := edc.content
	defer func() { edc.content = content }()

	// may split token at cursor
	var tok, pre, post Token
	if at.conti < len(content) {
		tok = content[at.conti]
		pre = tok.Slice(0, at.off)
		post = tok.Slice(at.off, -1)
	}

	// allocate new content slots
	{
		allot := len(tokens)
		// re-use prior token slot
		if !tok.Empty() {
			allot--
		}
		// need a header slot
		if !pre.Empty() {
			allot++
		}
		// need a trailer slot
		if !post.Empty() {
			allot++
		}
		content = append(content, make([]Token, allot)...)
	}

	i := at.conti

	// make room and shift trailing tokens
	{
		shift := len(tokens)
		// prior token has a header remnant
		if !pre.Empty() {
			shift++
		}
		if j := i + 1 + shift; j < len(content) {
			copy(content[j:], content[i+1:])
		}
	}

	// update split token(s) and insert
	if !pre.Empty() {
		content[i] = pre
		i++
	}
	i += copy(content[i:], tokens)
	if !post.Empty() {
		content[i] = post
	}

	// update cursors
	k := i - at.conti
	off := 0
	for _, tok := range tokens {
		off += tok.Len()
	}
	for id, cur := range edc.cursors {
		if cur.conti > at.conti {
			cur.conti += k
			cur.loc += off
		} else if cur.conti == at.conti && cur.off >= at.off {
			cur.conti += k
			cur.loc += off
			cur.off -= pre.Len()
		}
		edc.cursors[id] = cur
	}
}

// Write stores p into the editor arena, and then inserts a content token.
func (edc Cursor) Write(p []byte) (int, error) {
	if _, err := edc.arena.Write(p); err != nil {
		return 0, err
	}
	edc.Insert(edc.arena.Take())
	return len(p), nil
}

// WriteString stores s into the editor arena, and then inserts a content token.
func (edc Cursor) WriteString(s string) (int, error) {
	if _, err := edc.arena.WriteString(s); err != nil {
		return 0, err
	}
	edc.Insert(edc.arena.Take())
	return len(s), nil
}

// Remove removes any content bytes that overlap with the given tokens.
func (ed *Editor) Remove(tokens ...Token) {
	content := ed.content
	tmp := ed.tmp[:0]
	defer func() {
		ed.tmp = content[:0]
		ed.content = tmp
	}()

	for _, tok := range content {
		for _, rm := range tokens {
			head, tail := tok.Sub(rm)
			if !head.Empty() {
				tmp = append(tmp, head)
			}
			if !tail.Empty() {
				tmp = append(tmp, tail)
			}
		}
	}
}

// Format writes user-friendly editor state under the %v verb with optional %+v multi-line verbosity.
func (ed *Editor) Format(f fmt.State, c rune) {
	switch c {
	case 'v':
		if !f.Flag('+') {
			fmt.Fprintf(f, "content:%q cursors:%v", ed.content, ed.cursors)
			return
		}
		first := true
		for i, tok := range ed.content {
			if first {
				first = false
			} else {
				f.Write([]byte{'\n'})
			}
			fmt.Fprintf(f, "content[%v]: %+v %q", i, tok, tok)
		}
		for id, at := range ed.cursors {
			if first {
				first = false
			} else {
				f.Write([]byte{'\n'})
			}
			fmt.Fprintf(f, "cursor#%v%+v", id, at)
		}

	default:
		fmt.Fprintf(f, "!(ERROR invalid format verb %%%s)", string(c))
	}
}

// Format writes user-friendly editor cursor state under the %v verb with optional %+v multi-line verbosity.
func (edc Cursor) Format(f fmt.State, c rune) {
	switch c {
	case 'v':
		fmt.Fprintf(f, "#%v", edc.id)
		subFmt(f, edc.cursors[edc.id])
	default:
		fmt.Fprintf(f, "!(ERROR invalid format verb %%%s)", string(c))
	}
}

func (at cursorData) Format(f fmt.State, c rune) {
	switch c {
	case 'v':
		fmt.Fprintf(f, "@%v", at.loc)
		if f.Flag('+') {
			fmt.Fprintf(f, "{cont:%v off:%v}", at.conti, at.off)
		}
	default:
		fmt.Fprintf(f, "!(ERROR invalid format verb %%%s)", string(c))
	}
}

func subFmt(f fmt.State, arg interface{}) {
	if f.Flag('+') {
		fmt.Fprintf(f, "%+v", arg)
	} else {
		fmt.Fprint(f, arg)
	}
}
