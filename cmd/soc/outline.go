package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"time"
	"unicode"

	"github.com/jcorbin/soc/internal/isotime"
	"github.com/jcorbin/soc/internal/scanio"
	"github.com/jcorbin/soc/scandown"
)

// outline represents document tree structure under a scandown.BlockStack scan,
// as defined by Headings, Lists, and Items; ignores any structure under
// Blockquote.
//
// Each outline item has a title populated from its first Paragraph (all bytes
// of a Heading). Each item may contribute to an ever narrowing time stamp.
// Most commonly, this will simply be a Heading with the current day; some
// example content:
//
// 	# 2020-08-11
//
// 	- something
// 	- this is the title
//
// 	  additional non-title content
//
// 	# 2020 a year section
//
// 	## 07 a month section
//
// 	### 04 a day section
//
// 	- 12:00 list item titles can also contribute to the time
//
// Should result in the following leaf [time]s and "title"s:
//
// 	[2020-08-11] "something"
// 	[2020-08-11] "this is the title"
// 	[2020] "a year section"
// 	[2020-07] "a month section"
// 	[2020-07-04] "a day section"
// 	[2020-07-04T12:00] "list item titles can also contribute to the time"
//
// See outlineScanner for an example code.
type outline struct {
	titled bool                    // true only if the last Scan recognized a new outline title
	id     []int                   // block ID
	block  []scandown.Block        // block definition
	time   []isotime.GrainedTime   // time parsed from block title prefixes
	title  []scanio.ByteArenaToken // title remnants (after time)
	arena  scanio.ByteArena        // title storage
}

// outlineScanner orchestrates a low level scanner, block stack, and outline.
// See outline for detail on what an outline adds to the block stack.
//
// Example:
//
// 	var out outlineScanner
// 	out.Reset(src)
// 	for out.Scan() {
// 		if out.titled {
// 			log.Println(out)
// 		}
// 	}
type outlineScanner struct {
	*bufio.Scanner
	block scandown.BlockStack
	outline
}

// Reset (re)initializes receiver state to scan a new outline from src.
func (sc *outlineScanner) Reset(src io.Reader) {
	sc.Scanner = bufio.NewScanner(src)
	sc.Scanner.Split(sc.block.Scan)
	sc.block.Reset()
	sc.truncate(0)
	sc.titled = false
}

// Scan scans another block from the source stream, and then updates outline
// state to reflect any newly encountered outline structure.
func (sc *outlineScanner) Scan() bool {
	if !sc.Scanner.Scan() {
		sc.truncate(0)
		sc.titled = false
		return false
	}
	sc.outline.sync(sc.block)
	return true
}

// lastTime returns the most fine-grained time parsed so far.
func (out outline) lastTime() (t isotime.GrainedTime) {
	if i := len(out.time) - 1; i >= 0 {
		t = out.time[i]
	}
	return t
}

// heading returns the n-th non-empty title token, and whether or not it's the
// current outline leaf (just scanned if out.titled).
//
// The n value can be though of as "effective level", which can differ
// substantially from any actual Heading levels involved.
//
// NOTE if a heading (or item) title contains only a date/time component, it
// does not count towards deepening the outline tree.
func (out outline) heading(n int) (_ scanio.ByteArenaToken, isLast bool) {
	m := 0
	for i := 0; i < len(out.title); i++ {
		if token := out.title[i]; !token.Empty() {
			if m++; m == n {
				return token, i+1 == len(out.title)
			}
		}
	}
	return scanio.ByteArenaToken{}, false
}

// Format provides a textual representation of the current outline state.
// Any time is printed as a prefix.
// Up to the first 50 characters of each title are then printed.
// When formatted with the "+" flag, also prints block and time data from each
// outline item.
func (out outline) Format(f fmt.State, _ rune) {
	first := true

	if !f.Flag('+') {
		if t := out.time[len(out.time)-1]; t.Grain() > 0 {
			fmt.Fprintf(f, "%v", t)
			first = false
		}
	}

	for i := range out.id {
		t := out.title[i]
		if !f.Flag('+') && t.Empty() {
			continue
		}

		if first {
			first = false
		} else {
			io.WriteString(f, " ")
		}

		if f.Flag('+') {
			fmt.Fprintf(f, "%v#%v", out.block[i], out.id[i])
		}

		if !t.Empty() {
			var trunc string
			tb := t.Bytes()
			if len(tb) > 50 {
				// TODO should be rune aware
				i := 50
				for i > 0 && tb[i] != ' ' {
					i--
				}
				tb = tb[:i]
				trunc = "..."
			}

			if f.Flag('+') {
				fmt.Fprintf(f, "(%q%s)", tb, trunc)
			} else {
				f.Write(tb)
				io.WriteString(f, trunc)
			}
		} else if f.Flag('+') {
			if t := out.time[i]; t.Grain() > 0 && (i == 0 || !t.Equal(out.time[i-1])) {
				fmt.Fprintf(f, "[%v]", t)
			}
		}
	}
}

func (out *outline) sync(blocks scandown.BlockStack) {
	var title scanio.ByteArenaToken
	out.titled = false

	i := 0 // <= len(out.id)

	// 1. if current block is a Heading, pop to its level and push it
	if head, _ := blocks.Head(); head.Type == scandown.Heading {
		level := head.Width
		for ; i < len(out.block); i++ {
			if b := out.block[i]; b.Type != scandown.Heading || b.Width >= level {
				out.truncate(i)
				break
			}
		}

		t := isotime.Time(time.Local, 0, 0, 0, 0, 0, 0)
		if j := i - 1; j >= 0 {
			t = out.time[j]
		}
		t, title = out.readTitle(t, &blocks)
		out.titled = true
		out.push(blocks.HeadID(), head, t, title)
		return
	}

	// 2. otherwise sync items since last heading
	for ; i < len(out.block); i++ {
		if out.block[i].Type != scandown.Heading {
			break
		}
	}
	j := 1 // <= blocks.Len()
	for ; j < blocks.Len(); i, j = i+1, j+1 {
		id := blocks.ID(j)
		if i < len(out.id) {
			if out.id[i] == id {
				continue
			}
			out.truncate(i)
		}
		switch b, _ := blocks.Block(j); b.Type {
		// track list structure...
		case scandown.List, scandown.Item:
			t := isotime.Time(time.Local, 0, 0, 0, 0, 0, 0)
			if j := i - 1; j >= 0 {
				t = out.time[j]
			}
			out.push(id, b, t, title)
			continue
		// ...filling in Item titles from their first paragraph
		case scandown.Paragraph:
			if j := i - 1; j >= 0 && out.title[j].Empty() {
				t := out.time[j]
				t, title = out.readTitle(t, &blocks)
				out.titled = true
				out.time[j] = t
				out.title[j] = title
			}
		}
		break
	}

	if i < len(out.id) {
		out.truncate(i)
	}
}

func (out *outline) push(id int, block scandown.Block, t isotime.GrainedTime, title scanio.ByteArenaToken) {
	out.id = append(out.id, id)
	out.block = append(out.block, block)
	out.time = append(out.time, t)
	out.title = append(out.title, title)
}

func (out *outline) truncate(i int) {
	out.id = out.id[:i]
	out.block = out.block[:i]
	out.time = out.time[:i]
	out.title = out.title[:i]
	out.arena.PruneTo(out.title)
}

func (out *outline) readTitle(t isotime.GrainedTime, r io.Reader) (isotime.GrainedTime, scanio.ByteArenaToken) {
	// TODO scan inline content => words
	sc := bufio.NewScanner(r) // TODO internal rescanner
	sc.Split(bufio.ScanWords)
	scanio.CopyScannerWith(&out.arena, sc, []byte{' '})
	title := out.arena.Take()

	// parse any date components from the title prefix
	{
		tb := title.Bytes()
		if st, rb, parsed := t.Parse(tb); parsed {
			parsedLen := len(tb) - len(rb)
			title = title.Slice(parsedLen, -1)
			t = st
		}
	}

	// trim title to just first sentence
	{
		// TODO better sentence truncation
		tb := title.Bytes()
		rb := tb
		for i := 1; i < len(rb); i++ {
			switch rb[i] {
			case '.':
				if j := i + 1; j < len(rb) && rb[j] != ' ' {
					continue
				}
				rb = rb[:i]
			case ';':
				rb = rb[:i]
			}
		}
		if trim := len(tb) - len(rb); trim > 0 {
			title = title.Slice(0, -trim)
		}
	}

	return t, title
}

// section represents a range within a document outline, containing a header
// (which provides any outline time and tille) and a, maybe empty, body of
// additional content.
//
// Example, search for some interesting sections:
//
//	var (
//		interesting []section
//		out         outlineScanner
//	)
// 	for out.Reset(src); out.Scan(); {
// 		for i, sec := range interesting {
// 			interesting[i] = out.updateSection(sec)
// 		}
// 		if out.titled && want(out) {
// 			interesting = append(interesting, out.openSection())
// 		}
// 	}
type section struct {
	byteRange
	body byteRange
	id   int
}

func (sec section) add(offset int64) section {
	sec.byteRange = sec.byteRange.add(offset)
	sec.body = sec.body.add(offset)
	return sec
}

func (sec section) header() byteRange {
	sec.end = sec.body.start
	return sec.byteRange
}

// openSection returns a new section whose heading is the current block just scanned.
// The section's id is anchored to the last Heading or Item block; for a
// Heading, this should be the same as the current block, but should differ for
// List Items.
// Returns the zero section if sc.titled is false.
func (sc *outlineScanner) openSection() (sec section) {
	if !sc.titled {
		return section{}
	}
	sec.start = sc.block.Offset()
	sec.body = sec.byteRange
	sec.body.start += int64(len(sc.Bytes()))
	sec.end = -1
	for i := len(sc.id) - 1; i >= 0; i-- {
		if isOneOfType(sc.outline.block[i].Type, scandown.Heading, scandown.Item) {
			sec.id = sc.id[i]
			break
		}
	}
	return sec
}

// within returns true only if the given section's id is part of the current
// outline path.
func (sc *outlineScanner) within(sec section) bool {
	for _, id := range sc.outline.id {
		if id == sec.id {
			return true
		}
	}
	return false
}

// updateSection updates the given section, ending it at the current scan
// offset if it was still open, but is no longer part of the current outline
// path.
func (sc *outlineScanner) updateSection(sec section) section {
	// skip if not open or ended
	if sec.id == 0 || sec.end >= 0 {
		return sec
	}

	// end if not within
	if !sc.within(sec) {
		sec.end = sc.block.Offset()
		sec.body.end = sec.end
		return sec
	}

	// trim initial Blank from body
	if id := sc.block.HeadID(); id == sec.id+1 {
		if b, _ := sc.block.Head(); b.Type == scandown.Blank {
			sec.body.start = sc.block.Offset() + int64(len(sc.Bytes()))
		}
	}

	return sec
}

func mustCompileOutlineFilter(args ...interface{}) outlineFilter {
	f, err := compileOutlineFilter(args...)
	if err != nil {
		panic(err)
	}
	return f
}

func compileOutlineFilter(args ...interface{}) (outlineFilter, error) {
	var fs outlineFilterAnd
	for _, arg := range args {
		switch val := arg.(type) {
		case bool:
			if !val {
				return outlineFilterConst(val), nil
			}

		case func(out *outline) bool:
			fs = append(fs, outlineFilterFunc(val))

		case isotime.TimeGrain:
			fs = append(fs, outlineTimeGrainFilter(val))

		case int:
			fs = append(fs, outlineLevelFilter(val))

		case outlineFilterAnd:
			fs = append(fs, val...)

		case outlineFilter:
			fs = append(fs, val)

		default:
			return nil, fmt.Errorf("invalid outline filter arg type %T", arg)
		}
	}

	switch len(fs) {
	case 0:
		return nil, nil
	case 1:
		return fs[0], nil
	default:
		return fs, nil
	}
}

func outlineFilters(filters ...outlineFilter) outlineFilter {
	var fs outlineFilterAnd
	for _, f := range filters {
		switch fv := f.(type) {
		case nil:
		case outlineFilterConst:
			if !bool(fv) {
				return fv // const false annihilates
			}
			// elide const true
		case outlineFilterAnd:
			fs = append(fs, fv...)
		default:
			fs = append(fs, fv)
		}
	}
	switch len(fs) {
	case 0:
		return nil
	case 1:
		return fs[0]
	default:
		return fs
	}
}

type outlineFilter interface{ match(out *outline) bool }
type outlineFilterConst bool
type outlineFilterAnd []outlineFilter
type outlineFilterFunc func(out *outline) bool
func (c outlineFilterConst) match(out *outline) bool { return bool(c) }
func (f outlineFilterFunc) match(out *outline) bool  { return f(out) }
func (fs outlineFilterAnd) match(out *outline) bool {
	for _, f := range fs {
		if !f.match(out) {
			return false
		}
	}
	return true
}

type outlineTimeGrainFilter isotime.TimeGrain
func (tg outlineTimeGrainFilter) match(out *outline) bool {
	return out.lastTime().Grain() >= isotime.TimeGrain(tg)
}

type outlineLevelFilter int
func (l outlineLevelFilter) match(out *outline) bool {
	_, is := out.heading(int(l))
	return is
}

func isOneOfType(t scandown.BlockType, oneOf ...scandown.BlockType) bool {
	for _, ot := range oneOf {
		if ot == t {
			return true
		}
	}
	return false
}

func printOutline(to io.Writer, from io.Reader, filters ...outlineFilter) error {
	var sc outlineScanner
	filter := outlineFilters(filters...)
	sc.Reset(from)

	var (
		id  []int
		n   []int
		w   []int
		t   []isotime.GrainedTime
		buf bytes.Buffer

		prior bool
	)
	buf.Grow(1024)
	for sc.Scan() {
		if !sc.titled {
			continue
		}
		if filter != nil && !filter.match(&sc.outline) {
			continue
		}

		// sync scanned ID state:
		// - id[i:] are the "exited" nodes, no longer on the scan stack
		// - sc.id[i:] are the "entered" nodes, new on the scan stack this round
		var i int
		id, i = updateIDs(id, sc.id)
		if i < len(n) {
			n = n[:i+1] // truncate exited levels, but carry level count
		} else {
			n = n[:i] // truncate exited levels
		}
		w = w[:i] // truncate exited widths
		t = t[:i] // truncate exited times

		// add entered nodes, printing lines
		for ; i < len(sc.id); i++ {
			if i < len(n) {
				n[i]++ // increment level carried from above truncation
			} else {
				n = append(n, 1) // start a new level count
			}
			w = append(w, 0) // width starts out 0, will be filled in if we format an ordinal

			level := 0
			nt := sc.time[i]
			t = append(t, nt)
			if nt.Grain() > 0 {
				if i == 0 {
					level++
				} else {
					for j := i - 1; j >= 0; j-- {
						ot := t[j]
						if ot.Equal(nt) {
							break
						}
						level++
						if ot.Grain() == 0 {
							break
						}
					}
				}
			}

			title := sc.title[i].Bytes()
			buf.Grow(len(title) / 4 * 5) // ensure 25% over allocation

			in := 0
			if level > 0 {
				if prior {
					buf.WriteByte('\n') // hard paragraph break
				}
				// write a temporal header item
				for i := 0; i < level; i++ {
					buf.WriteByte('#')
				}
				buf.WriteByte(' ')
				fmt.Fprint(&buf, nt)
			} else if len(title) == 0 {
				continue
			} else {
				in = sumInts(w)
			}

			for i := 0; i < in; i++ {
				buf.WriteByte(' ')
			}
			var nw int
			if level == 0 {
				// write an ordinal bullet item
				nw, _ = fmt.Fprintf(&buf, "%v. ", n[i])
			}
			const lineWidth = 80
			title = breakLineInto(&buf, title, lineWidth)
			in += nw
			w[i] = nw
			for len(title) > 0 {
				for i := 0; i < in; i++ {
					buf.WriteByte(' ')
				}
				title = breakLineInto(&buf, title, lineWidth)
			}

			// flush formatted item buffer
			prior = true
			if _, err := buf.WriteTo(to); err != nil {
				return err
			}
		}
	}

	return sc.Err()
}

func breakLineInto(buf *bytes.Buffer, b []byte, width int) []byte {
	var line []byte
	if line = b; len(line) > width {
		i := width
		if i = bytes.LastIndexFunc(line[:i+1], isNonWord); i < 0 {
			i = bytes.IndexFunc(line, isNonWord)
		}
		if i > 0 {
			line = line[:i]
		}
	}
	buf.Write(line)
	buf.WriteByte('\n')
	return b[len(line):]
}

func isNonWord(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsPunct(r)
}

func updateIDs(into, from []int) (_ []int, prefix int) {
	prefix = commonPrefix(into, from)
	if prefix < len(into) {
		into = into[:prefix]
	}
	into = append(into, from[prefix:]...)
	return into, prefix
}

func commonPrefix(a, b []int) (i int) {
	for i < len(a) && i < len(b) {
		if a[i] != b[i] {
			break
		}
		i++
	}
	return i
}

func sumInts(ns []int) (t int) {
	for _, n := range ns {
		t += n
	}
	return t
}
