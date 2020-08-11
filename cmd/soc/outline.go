package main

import (
	"bufio"
	"fmt"
	"io"
	"time"

	"github.com/jcorbin/soc/internal/isotime"
	"github.com/jcorbin/soc/internal/scanio"
	"github.com/jcorbin/soc/scandown"
)

type outline struct {
	titled bool                    // true only if the last Scan recognized a new outline title
	id     []int                   // block ID
	block  []scandown.Block        // block definition
	time   []isotime.GrainedTime   // time parsed from block title prefixes
	title  []scanio.ByteArenaToken // title remnants (after time)
	arena  scanio.ByteArena        // title storage
}

type outlineScanner struct {
	*bufio.Scanner
	block scandown.BlockStack
	outline
}

func (sc *outlineScanner) Reset(src io.Reader) {
	sc.Scanner = bufio.NewScanner(src)
	sc.Scanner.Split(sc.block.Scan)
	sc.block.Reset()
	sc.truncate(0)
	sc.titled = false
}

func (sc *outlineScanner) Scan() bool {
	if !sc.Scanner.Scan() {
		sc.truncate(0)
		sc.titled = false
		return false
	}
	sc.outline.sync(sc.block)
	return true
}

func (out outline) lastTime() (t isotime.GrainedTime) {
	if i := len(out.time) - 1; i >= 0 {
		t = out.time[i]
	}
	return t
}

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

func (out outline) Format(f fmt.State, _ rune) {
	first := true

	if !f.Flag('+') {
		if t := out.time[len(out.time)-1]; t.Grain() > 0 {
			fmt.Fprintf(f, "[%v]", t)
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
	scanio.CopyTokensWith(&out.arena, sc, []byte{' '})
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

type section struct {
	byteRange
	body byteRange
	id   int
}

func (sec section) header() byteRange {
	sec.end = sec.body.start
	return sec.byteRange
}

func (sc *outlineScanner) openSection(ofType ...scandown.BlockType) (sec section) {
	sec.start = sc.block.Offset()
	sec.body = sec.byteRange
	sec.body.start += int64(len(sc.Bytes()))
	sec.end = -1
	for i := len(sc.id) - 1; i >= 0; i-- {
		if len(ofType) == 0 || isOneOfType(sc.outline.block[i].Type, ofType...) {
			sec.id = sc.id[i]
			break
		}
	}
	return sec
}

func (sc *outlineScanner) within(sec section) bool {
	for _, id := range sc.outline.id {
		if id == sec.id {
			return true
		}
	}
	return false
}

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
