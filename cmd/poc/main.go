// Command poc is a proof-of-concept for Stream-Of-Consciousness ( SoC ).
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
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
	var buf bytes.Buffer
	buf.Grow(4096)
	walkOutline(node, func(visit outlineVistData) (status blackfriday.WalkStatus) {
		if !visit.Entering() {
			return status
		}

		if skip.AnyString(visit.Title(visit.Len() - 1)) {
			return blackfriday.SkipChildren
		}

		var t itemTime
		t.loc = time.Local
		buf.Reset()

		defer func() {
			if t.Any() {
				_, err = fmt.Fprintf(out, "%v %s\n", t, buf.Bytes())
			} else {
				_, err = fmt.Fprintf(out, "%s\n", buf.Bytes())
			}
			if err != nil {
				status = blackfriday.Terminate
			}
		}()

		for i := 0; i < visit.Len(); i++ {
			node := visit.Node(i)
			title := visit.Title(i)
			title = t.Parse(title)
			if title == "" {
				continue
			}

			if buf.Len() > 0 {
				buf.WriteString(" ")
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
			buf.WriteString(title)
			buf.WriteByte(']')
		}

		return status
	})
	return err
}

type outlineVistData interface {
	Entering() bool
	Len() int
	Node(i int) *blackfriday.Node
	Title(i int) string
}

type outlineVisitor func(visit outlineVistData) blackfriday.WalkStatus

func walkOutline(node *blackfriday.Node, visitor outlineVisitor) {
	var o outlineWalker
	o.tmp.Grow(1024)
	o.walk(node, visitor)
}

type outlineWalker struct {
	// TODO wants to extend blackfriday.nodeWalker rather than wrap blackfriday.Node.Walk
	path  []*blackfriday.Node
	title []string
	skip  []bool

	tmp bytes.Buffer
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

type outlineWalkerData struct {
	entering bool
	path     []*blackfriday.Node
	title    []string

	tmp *bytes.Buffer
}

func (wd outlineWalkerData) Entering() bool               { return wd.entering }
func (wd outlineWalkerData) Len() int                     { return len(wd.path) }
func (wd outlineWalkerData) Node(i int) *blackfriday.Node { return wd.path[i] }
func (wd outlineWalkerData) Title(i int) string {
	t := wd.title[i]
	if t == "" {
		wd.tmp.Reset()
		collectTitle(wd.tmp, wd.path[i])
		t = wd.tmp.String()
		wd.title[i] = t
	}
	return t
}

func (o *outlineWalker) enter(node *blackfriday.Node, visitor outlineVisitor) (status blackfriday.WalkStatus) {
	var skip bool
	i := len(o.skip)
	if j := i - 1; j >= 0 {
		skip = o.skip[j]
	}
	o.path = append(o.path, node)
	o.title = append(o.title, "")
	o.skip = append(o.skip, skip)
	if !skip {
		status = visitor(outlineWalkerData{true, o.path, o.title, &o.tmp})
		if status == blackfriday.SkipChildren {
			o.skip[i] = true
		}
	}
	return status
}

func (o *outlineWalker) exitTo(i int, visitor outlineVisitor) blackfriday.WalkStatus {
	defer func() {
		o.path = o.path[:i]
		o.title = o.title[:i]
		o.skip = o.skip[:i]
	}()
	for j := len(o.path) - 1; j >= 0 && i <= j; j-- {
		if o.skip[j] {
			continue
		}
		if st := visitor(outlineWalkerData{false, o.path[:j+1], o.title, &o.tmp}); st >= blackfriday.Terminate {
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

func (pl *patternList) AnyString(s string) bool {
	for _, p := range pl.patterns {
		if p.MatchString(s) {
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

type timeLevel uint

const (
	timeLevelNone timeLevel = iota
	timeLevelYear
	timeLevelMonth
	timeLevelDay
	timeLevelHour
	timeLevelMinute
)

type itemTime struct {
	level  timeLevel
	year   int
	month  time.Month
	day    int
	hour   int
	minute int
	loc    *time.Location
}

func (t itemTime) Any() bool {
	return t.level > timeLevelNone
}

func (t itemTime) Time() time.Time {
	switch t.level {
	case timeLevelNone:
	case timeLevelYear:
		return time.Date(t.year, 1, 1, 0, 0, 0, 0, t.loc)
	case timeLevelMonth:
		return time.Date(t.year, t.month, 1, 0, 0, 0, 0, t.loc)
	case timeLevelDay:
		return time.Date(t.year, t.month, t.day, 0, 0, 0, 0, t.loc)
	case timeLevelHour:
		return time.Date(t.year, t.month, t.day, t.hour, 0, 0, 0, t.loc)
	case timeLevelMinute:
		return time.Date(t.year, t.month, t.day, t.hour, t.minute, 0, 0, t.loc)
	}
	return time.Time{}
}

func (t itemTime) String() string {
	tt := t.Time()
	switch t.level {
	case timeLevelNone:
	case timeLevelYear:
		return tt.Format("2006")
	case timeLevelMonth:
		return tt.Format("2006-01")
	case timeLevelDay:
		return tt.Format("2006-01-02")
	case timeLevelHour:
		return tt.Format("2006-01-02T15Z07")
	case timeLevelMinute:
		return tt.Format("2006-01-02T15:04Z07")
	}
	return ""
}

func (t *itemTime) Parse(s string) (rest string) {
	if t.level >= timeLevelMinute {
		return s
	}

	if rest == "" {
		rest = strings.TrimLeftFunc(s, unicode.IsSpace)
	}
	for len(rest) > 0 && t.level < timeLevelMinute {
		next := strings.TrimLeft(rest, " ")
		if t.level < timeLevelHour {
			if next[0] == '-' || next[0] == '/' {
				next = next[1:]
			}
		} else {
			if next[0] == ':' {
				next = next[1:]
			}
		}

		i := 0
		for i < len(next) && '0' <= next[i] && next[i] <= '9' {
			i++
		}
		part := next[:i]
		next = next[i:]

		num, err := strconv.Atoi(part)
		if err != nil {
			return rest
		}

		switch t.level {
		case timeLevelNone:
			t.year = num

		case timeLevelYear:
			if num == 0 || num > 12 {
				return rest
			}
			t.month = time.Month(num)

		case timeLevelMonth:
			// TODO stricter max day-of-month logic
			if num == 0 || num > 31 {
				return rest
			}
			t.day = num

		case timeLevelDay:
			if num > 24 {
				return rest
			}
			t.hour = num

		case timeLevelHour:
			if num > 24 {
				return rest
			}
			t.minute = num

		}
		t.level++

		rest = next
	}
	return rest
}
