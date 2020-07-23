package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jcorbin/soc/internal/isotime"
	"github.com/jcorbin/soc/internal/scanio"
	"github.com/jcorbin/soc/internal/socui"
	"github.com/jcorbin/soc/scandown"
)

func init() {
	builtinServer("list", serveList,
		"print stream outline listing")
}

func serveList(ctx context, _ *socui.Request, resp *socui.Response) error {
	rc, err := ctx.store.open()
	if errors.Is(err, errStoreNotExists) {
		return fmt.Errorf("%w; run `soc init` to create one", err)
	} else if err != nil {
		return err
	}

	var blocks scandown.BlockStack
	sc := bufio.NewScanner(rc)
	sc.Split(blocks.Scan)
	var out outline
	n := 0
	for sc.Scan() {
		if out.sync(blocks) {
			if out.lastTime().Grain() == 0 {
				continue
			}
			if !out.toplevel() {
				continue
			}
			n++
			fmt.Fprintf(resp, "%v. %v\n", n, out)
		}
	}

	return rc.Close()
}

type outline struct {
	id    []int
	block []scandown.Block
	time  []isotime.GrainedTime
	title scanio.ByteTokens
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

func (out *outline) sync(blocks scandown.BlockStack) (titled bool) {
	var title scanio.ByteArenaToken
	defer func() { titled = !title.Empty() }()

	i := 0 // <= len(out.id)

	// 1. if current block is a Heading, pop to its level and push it
	if id, head, _ := blocks.Head(); head.Type == scandown.Heading {
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
		out.push(id, head, t, title)
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
		id, b, _ := blocks.Block(j)
		if i < len(out.id) {
			if out.id[i] == id {
				continue
			}
			out.truncate(i)
		}
		switch b.Type {
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
				out.time[j] = t
				out.title.Set(j, title)
			}
		}
		break
	}

	if i < len(out.id) {
		out.truncate(i)
	}
	return
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
