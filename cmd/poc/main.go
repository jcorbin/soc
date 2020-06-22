// Command poc is a proof-of-concept for Stream-Of-Consciousness ( SoC ).
package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"

	"github.com/russross/blackfriday"
)

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(in, out *os.File) error {
	b, err := ioutil.ReadAll(in)
	if err != nil {
		return err
	}

	md := blackfriday.New()
	doc := md.Parse(b)

	var p proc
	p.tmp.Grow(4096)
	return p.scanOutline(doc, out)
}

type proc struct {
	level    []int
	isHeader []bool
	title    []string

	tmp bytes.Buffer
}

func (p *proc) scanOutline(n *blackfriday.Node, out io.Writer) (err error) {
	n.Walk(func(n *blackfriday.Node, entering bool) (status blackfriday.WalkStatus) {
		if !entering {
			return blackfriday.GoToNext
		}
		switch n.Type {

		case blackfriday.Heading:
			status = blackfriday.SkipChildren
			p.outoHeaderLevel(n.Level - 1)
			p.collectText(n)
			p.into(n.Level, true, p.tmp.String())

			// TODO recognize lists

			p.tmp.Reset()
			for i := range p.level {
				if p.tmp.Len() > 0 {
					p.tmp.WriteString(" > ")
				}
				if p.isHeader[i] {
					p.tmp.WriteByte('#')
				} else {
					p.tmp.WriteByte('-')
				}
				p.tmp.WriteByte('[')
				p.tmp.WriteString(p.title[i])
				p.tmp.WriteByte(']')
			}
			_, err = fmt.Fprintf(out, "%s\n", p.tmp.Bytes())

		}
		if err != nil {
			return blackfriday.Terminate
		}
		return status
	})
	return err
}

func (p *proc) outoLevel(level int) {
	p.outo(func(i int) bool { return p.level[i] <= level })
}

func (p *proc) outoHeaderLevel(level int) {
	p.outo(func(i int) bool { return p.level[i] <= level && p.isHeader[i] })
}

func (p *proc) outo(whence func(i int) bool) {
	i := len(p.level) - 1
	for i >= 0 {
		if whence(i) {
			break
		}
		i--
	}
	p.truncate(i + 1)
}

func (p *proc) into(level int, isHeader bool, title string) {
	p.level = append(p.level, level)
	p.isHeader = append(p.isHeader, isHeader)
	p.title = append(p.title, title)
}

func (p *proc) truncate(i int) {
	p.level = p.level[:i]
	p.isHeader = p.isHeader[:i]
	p.title = p.title[:i]
}

func (p *proc) collectText(n *blackfriday.Node) {
	p.tmp.Reset()
	n.Walk(func(n *blackfriday.Node, entering bool) blackfriday.WalkStatus {
		if entering {
			// TODO type aware things, e.g. just link titles
			p.tmp.Write(n.Literal)
		}
		return blackfriday.GoToNext
	})
}
