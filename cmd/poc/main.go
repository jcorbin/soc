// Command poc is a proof-of-concept for Stream-Of-Consciousness ( SoC ).
package main

import (
	"bytes"
	"io"
	"io/ioutil"
	"log"
	"os"
	"unicode"

	"github.com/russross/blackfriday"
)

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(in, out *os.File) error {
	md := blackfriday.New()

	b, err := ioutil.ReadAll(in)
	if err != nil {
		return err
	}
	doc := md.Parse(b)

	return scanOutline(doc, out)
}

func scanOutline(node *blackfriday.Node, out io.Writer) (err error) {
	var tmp bytes.Buffer
	tmp.Grow(4096)

	walkOutline(node, func(path []*blackfriday.Node, entering bool) blackfriday.WalkStatus {
		if !entering {
			return blackfriday.GoToNext
		}

		for i, node := range path {
			if i > 0 {
				tmp.WriteString(" > ")
			}
			switch node.Type {
			case blackfriday.Heading:
				tmp.WriteByte('#')
			case blackfriday.Item:
				tmp.WriteByte('-')
			default:
				tmp.WriteByte('?')
			}
			tmp.WriteByte('[')
			collectTitle(&tmp, node)
			tmp.WriteByte(']')
		}
		tmp.WriteByte('\n')

		if _, err = tmp.WriteTo(out); err != nil {
			return blackfriday.Terminate
		}

		return blackfriday.GoToNext
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

func (o *outlineWalker) walk(node *blackfriday.Node, visitor outlineVisitor) {
	node.Walk(func(n *blackfriday.Node, entering bool) blackfriday.WalkStatus {
		switch n.Type {

		case blackfriday.Document:
			return blackfriday.GoToNext

		case blackfriday.Heading:
			if !entering {
				return blackfriday.GoToNext
			}

			if i := o.find(func(i int) bool {
				return o.path[i].Type == blackfriday.Heading && o.path[i].Level < n.Level
			}) + 1; i < len(o.path) {
				o.path = o.path[:i]
				if st := visitor(o.path, false); st >= blackfriday.Terminate {
					return st
				}
			}

			o.path = append(o.path, n)
			return maxStatus(blackfriday.SkipChildren, visitor(o.path, true))

		case blackfriday.List:
			// TODO definition list semantics?
			return blackfriday.GoToNext

		case blackfriday.Item:
			if !entering {
				if i := o.find(func(i int) bool { return o.path[i] == n }); i < len(o.path) {
					if i < 0 {
						i = 0
					}
					o.path = o.path[:i]
					if st := visitor(o.path, false); st >= blackfriday.Terminate {
						return st
					}
				}
				return blackfriday.GoToNext
			}

			o.path = append(o.path, n)
			return visitor(o.path, true)

		default:
			// _, err = fmt.Fprintf(out, "SKIP entering:%v %v <- %v\n", entering, n, n.Parent)
			return blackfriday.SkipChildren
		}
	})
}

func maxStatus(a, b blackfriday.WalkStatus) blackfriday.WalkStatus {
	if b > a {
		return b
	}
	return a
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
