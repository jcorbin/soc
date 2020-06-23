// Command poc is a proof-of-concept for Stream-Of-Consciousness ( SoC ).
package main

import (
	"bytes"
	"flag"
	"io/ioutil"
	"log"
	"os"
	"regexp"
	"strings"
	"unicode"

	"github.com/russross/blackfriday"
)

var (
	in  = os.Stdin
	out = os.Stdout

	skip patternList
)

func main() {
	flag.Var(&skip, "skip", "skip outline items that match any given pattern")
	flag.Parse()

	var (
		err error
		b   []byte
	)
	b, err = ioutil.ReadAll(in)
	if err == nil {
		md := blackfriday.New()
		doc := md.Parse(b)
		err = scanOutline(doc)
	}

	if err != nil {
		log.Fatal(err)
	}
}

func scanOutline(node *blackfriday.Node) (err error) {
	var nom, buf bytes.Buffer
	nom.Grow(1024)
	buf.Grow(4096)
	walkOutline(node, func(path []*blackfriday.Node, entering bool) (status blackfriday.WalkStatus) {
		defer func() {
			if _, err = buf.WriteTo(out); err != nil {
				status = blackfriday.Terminate
			}
		}()

		nom.Reset()
		collectTitle(&nom, path[len(path)-1])

		if entering {
			if skip.Any(nom.Bytes()) {
				status = blackfriday.SkipChildren
				buf.WriteString("ESKIP ")
			} else {
				buf.WriteString("ENTER ")
			}
		} else {
			buf.WriteString(" EXIT ")
		}

		for i, node := range path {
			if i > 0 {
				buf.WriteString(" > ")
			}
			switch node.Type {
			case blackfriday.Heading:
				buf.WriteByte('#')
			case blackfriday.Item:
				buf.WriteByte('-')
			default:
				buf.WriteByte('?')
			}
			buf.WriteByte('[')
			collectTitle(&buf, node)
			buf.WriteByte(']')
		}
		buf.WriteByte('\n')
		return status
	})
	return err
}

type outlineVisitor func(path []*blackfriday.Node, entering bool) blackfriday.WalkStatus

func walkOutline(node *blackfriday.Node, visitor outlineVisitor) {
	var o outlineWalker
	o.walk(node, visitor)
}

type outlineWalker struct {
	// TODO wants to extend blackfriday.nodeWalker rather than wrap blackfriday.Node.Walk
	path []*blackfriday.Node
	skip []bool
}

func (o *outlineWalker) find(where func(i int) bool) int {
	i := len(o.path) - 1
	for i >= 0 {
		if where(i) {
			break
		}
		i--
	}
	return i
}

func (o *outlineWalker) enter(node *blackfriday.Node, visitor outlineVisitor) (status blackfriday.WalkStatus) {
	var skip bool
	i := len(o.skip)
	if j := i - 1; j >= 0 {
		skip = o.skip[j]
	}
	o.path = append(o.path, node)
	o.skip = append(o.skip, skip)
	if !skip {
		status = visitor(o.path, true)
		if status == blackfriday.SkipChildren {
			o.skip[i] = true
		}
	}
	return status
}

func (o *outlineWalker) exitTo(i int, visitor outlineVisitor) blackfriday.WalkStatus {
	defer func() {
		o.path = o.path[:i]
		o.skip = o.skip[:i]
	}()
	for j := len(o.path) - 1; j >= 0 && i <= j; j-- {
		if o.skip[j] {
			continue
		}
		if st := visitor(o.path[:j+1], false); st >= blackfriday.Terminate {
			return st
		}
	}
	return blackfriday.GoToNext
}

func (o *outlineWalker) walk(node *blackfriday.Node, visitor outlineVisitor) {
	defer o.exitTo(0, visitor)
	node.Walk(func(n *blackfriday.Node, entering bool) blackfriday.WalkStatus {
		switch n.Type {

		case blackfriday.Document:
			return blackfriday.GoToNext

		case blackfriday.Heading:
			if !entering {
				return blackfriday.GoToNext
			}
			if st := o.exitTo(o.find(func(i int) bool {
				return o.path[i].Type == blackfriday.Heading && o.path[i].Level < n.Level
			})+1, visitor); st >= blackfriday.Terminate {
				return st
			}
			if st := o.enter(n, visitor); st > blackfriday.GoToNext {
				return st
			}
			return blackfriday.SkipChildren

		case blackfriday.List:
			// TODO definition list semantics?
			return blackfriday.GoToNext

		case blackfriday.Item:
			if !entering {
				return o.exitTo(o.find(func(i int) bool { return o.path[i] == n }), visitor)
			}
			return o.enter(n, visitor)

		default:
			// _, err = fmt.Fprintf(out, "SKIP entering:%v %v <- %v\n", entering, n, n.Parent)
			return blackfriday.SkipChildren
		}
	})
}

func collectTitle(buf *bytes.Buffer, node *blackfriday.Node) {
	if node == nil {
		buf.WriteString("<NilNode>")
		return
	}
	switch node.Type {

	case blackfriday.Document:
		node.Walk(func(n *blackfriday.Node, entering bool) blackfriday.WalkStatus {
			if entering && (n.Type == blackfriday.Heading || n.Type == blackfriday.Item) {
				collectItemTitle(buf, n)
				return blackfriday.Terminate
			}
			return blackfriday.GoToNext
		})

	case blackfriday.List:
		node.Walk(func(n *blackfriday.Node, entering bool) blackfriday.WalkStatus {
			if entering && n.Type == blackfriday.Item {
				collectItemTitle(buf, n)
				return blackfriday.Terminate
			}
			return blackfriday.GoToNext
		})

	case blackfriday.Item, blackfriday.Heading:
		collectItemTitle(buf, node)

	// TODO should make tables (resp rows) equivalent to lists (resp items)?
	// TODO maybe parse subject line from code blocks? use info?
	// TODO maybe parse subject line from block quotes?
	// TODO maybe parse first sentence from paragraphs?
	// TODO maybe parse structure from html blocks?

	default:
		buf.WriteString("<Unsupported")
		buf.WriteString(node.Type.String())
		buf.WriteString(">")
	}
}

func collectItemTitle(buf *bytes.Buffer, node *blackfriday.Node) {
	startLen := buf.Len()

	node.Walk(func(n *blackfriday.Node, entering bool) blackfriday.WalkStatus {
		switch n.Type {

		case blackfriday.Document, blackfriday.List, blackfriday.Heading:
			if n != node {
				return blackfriday.Terminate
			}
			if !entering {
				return blackfriday.Terminate
			}
			return blackfriday.GoToNext

		case blackfriday.CodeBlock, blackfriday.HTMLBlock,
			blackfriday.Table, blackfriday.TableCell, blackfriday.TableHead, blackfriday.TableBody, blackfriday.TableRow:
			return blackfriday.Terminate

		case blackfriday.Paragraph, blackfriday.Item, blackfriday.BlockQuote:
			if buf.Len() > startLen {
				return blackfriday.Terminate
			}
			return blackfriday.GoToNext

		// TODO support horizontal rule fencing?

		case blackfriday.Softbreak, blackfriday.Hardbreak:
			if buf.Len() > startLen {
				return blackfriday.Terminate
			}
			return blackfriday.GoToNext

		// TODO need special support for link, image, or html content?
		default:
			status := blackfriday.GoToNext
			if entering {
				b := n.Literal
				if buf.Len() == startLen {
					b = bytes.TrimLeftFunc(b, unicode.IsSpace)
				} else if i := bytes.IndexByte(b, '\n'); i >= 0 {
					b = b[:i]
					status = blackfriday.Terminate
				}
				buf.Write(b)
			}
			return status
		}
	})
}

type patternList struct {
	patterns []*regexp.Regexp
}

func (pl *patternList) Any(b []byte) bool {
	for _, p := range pl.patterns {
		if p.Match(b) {
			return true
		}
	}
	return false
}

func (pl *patternList) String() string {
	if pl == nil {
		return ""
	}
	var parts []string
	for _, p := range pl.patterns {
		parts = append(parts, p.String())
	}
	return strings.Join(parts, " ")
}

func (pl *patternList) Set(s string) error {
	if s == "" {
		return nil
	}
	p, err := regexp.Compile(s)
	if err == nil {
		pl.patterns = append(pl.patterns, p)
	}
	return err
}
