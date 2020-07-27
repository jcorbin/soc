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

type outlineScanner struct {
	*bufio.Scanner
	block scandown.BlockStack
	outline
}

func (sc *outlineScanner) Reset(r io.Reader) {
	sc.Scanner = bufio.NewScanner(r)
	sc.Scanner.Split(sc.block.Scan)
}

func (sc *outlineScanner) Scan() bool {
	if !sc.Scanner.Scan() {
		sc.truncate(0)
		return false
	}
	sc.outline.sync(sc.block)
	return true
}

type outline struct {
	id     []int
	block  []scandown.Block
	time   []isotime.GrainedTime
	title  scanio.ByteTokens
	titled bool
}

func (out outline) toplevel() bool {
	for i := 0; i < out.title.Len(); i++ {
		if token := out.title.Get(i); !token.Empty() {
			return i+1 == out.title.Len()
		}
	}
	return false
}

func (out outline) firstTitle() scanio.ByteArenaToken {
	for i := 0; i < out.title.Len(); i++ {
		if token := out.title.Get(i); !token.Empty() {
			return token
		}
	}
	return scanio.ByteArenaToken{}
}

func (out outline) lastTime() (t isotime.GrainedTime) {
	if i := len(out.time) - 1; i >= 0 {
		t = out.time[i]
	}
	return t
}

func (out outline) Format(f fmt.State, _ rune) {
	first := true

	if t := out.time[len(out.time)-1]; t.Grain() > 0 {
		fmt.Fprintf(f, "[%v]", t)
		first = false
	}

	for i := range out.id {
		if t := out.title.Get(i); !t.Empty() {
			if first {
				first = false
			} else {
				io.WriteString(f, " ")
			}

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
				fmt.Fprintf(f, "%v(%q%s)", out.block[i], tb, trunc)
			} else {
				f.Write(tb)
				io.WriteString(f, trunc)
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
			if j := i - 1; j >= 0 && out.title.Get(j).Empty() {
				t := out.time[j]
				t, title = out.readTitle(t, &blocks)
				out.titled = true
				out.time[j] = t
				out.title.Set(j, title)
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
	out.title.Push(title)
}

func (out *outline) truncate(i int) {
	out.id = out.id[:i]
	out.block = out.block[:i]
	out.time = out.time[:i]
	out.title.Truncate(i)
}

func (out *outline) readTitle(t isotime.GrainedTime, r io.Reader) (isotime.GrainedTime, scanio.ByteArenaToken) {
	// TODO scan inline content => words
	sc := bufio.NewScanner(r) // TODO internal rescanner
	sc.Split(bufio.ScanWords)
	scanio.CopyTokensWith(&out.title, sc, []byte{' '})
	title := out.title.Take()

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
