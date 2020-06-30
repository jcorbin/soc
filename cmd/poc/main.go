// Command poc is a proof-of-concept for Stream-Of-Consciousness ( SoC ).
package main

import (
	"bytes"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/russross/blackfriday"
)

func main() {
	var ui streamUIContext
	ui.now = time.Now()
	ui.out = os.Stdout
	ui.buf.Grow(4096)

	ui.md = blackfriday.New(blackfriday.WithExtensions(0 |
		blackfriday.NoIntraEmphasis |
		// blackfriday.DefinitionLists |
		// blackfriday.Tables |
		blackfriday.FencedCode |
		blackfriday.Autolink |
		blackfriday.Strikethrough |
		blackfriday.SpaceHeadings |
		blackfriday.HeadingIDs |
		blackfriday.BackslashLineBreak,
	))

	if err := ui.readStream(os.Stdin); err != nil {
		log.Fatal(err)
	}

	ui.rollover(ui.now)
	// stream.outputOutline(ui.doc)
	if err := ui.outputMarkdown(ui.doc); err != nil {
		log.Fatal(err)
	}
}

type streamUIContext struct {
	streamInput
	stream
	streamOutput
}

type streamInput struct {
	md  *blackfriday.Markdown
	now time.Time
}

type streamOutput struct {
	bufWriter
}

type stream struct {
	doc *blackfriday.Node
}

func (ui *streamUIContext) readStream(r io.Reader) error {
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}
	ui.doc = ui.md.Parse(b)
	return nil
}

func (sout *streamOutput) outputOutline(node *blackfriday.Node) error {
	return writeOutline(&sout.bufWriter, node)
}

func (sout *streamOutput) outputMarkdown(node *blackfriday.Node) error {
	return writeMarkdown(&sout.bufWriter, node)
}

func (stream *stream) rollover(now time.Time) {
	var (
		today       = itemDate(now.Date())
		firstDay    *blackfriday.Node
		todayNode   *blackfriday.Node
		horizonNode *blackfriday.Node
	)

	walkOutline(stream.doc, func(visit outlineVistData) blackfriday.WalkStatus {
		if !visit.Entering() {
			return blackfriday.GoToNext
		}

		if isTemporal(visit) {
			if firstDay == nil && visit.Time().level == timeLevelDay {
				firstDay = visit.Node(visit.Len() - 1)
			}
			if visit.Time().Equal(today) {
				todayNode = visit.Node(visit.Len() - 1)
			}
		} else if sectionDepth(visit, "Done") == 1 {
			horizonNode = visit.Node(visit.Len() - 1)
		}

		return blackfriday.GoToNext
	})

	if todayNode == nil {
		todayNode = blackfriday.NewNode(blackfriday.Heading)
		todayNode.Level = 1
		if firstDay != nil {
			todayNode.Level = firstDay.Level
		}

		// TODO support generating a parent-relative suffix string
		text := blackfriday.NewNode(blackfriday.Text)
		text.Literal = []byte(today.String())
		todayNode.AppendChild(text)

		firstDay.InsertBefore(todayNode)
	}
	firstDay.Unlink()
	horizonNode.Next.InsertBefore(firstDay)
}

func writeOutline(bw *bufWriter, node *blackfriday.Node) (err error) {
	defer bw.finalWrite(&err)
	walkOutline(node, func(visit outlineVistData) (status blackfriday.WalkStatus) {
		defer bw.walkFlush(&err, &status)

		if visit.Entering() {
			bw.buf.WriteString(visit.Time().String())

			// build path with pure time components elided
			for i := 0; i < visit.Len(); i++ {
				title := visit.Title(i)
				if title == "" {
					continue
				}

				if bw.buf.Len() > 0 {
					bw.buf.WriteString(" ")
				}

				switch visit.Node(i).Type {
				case blackfriday.Heading:
					bw.buf.WriteByte('#')
				case blackfriday.Item:
					bw.buf.WriteByte('-')
				default:
					bw.buf.WriteByte('?')
				}
				bw.buf.WriteByte('[')
				bw.buf.WriteString(title)
				bw.buf.WriteByte(']')
			}

			bw.buf.WriteByte('\n')
		}

		return blackfriday.GoToNext
	})
	return err
}

type outlineVistData interface {
	Entering() bool
	Time() itemTime
	Len() int
	Node(i int) *blackfriday.Node
	Title(i int) string
}

// isTemporal return true only if the visited item has a time set, but no title
// strings.
func isTemporal(visit outlineVistData) bool {
	if !visit.Time().Any() {
		return false
	}
	for i := 0; i < visit.Len(); i++ {
		if visit.Title(i) != "" {
			return false
		}
	}
	return true
}

// sectionDepth returns a depth level indicating how far under a named section
// the visited item is: return 0 if not within, 1 for the section item itself,
// and a value >1 for any sub-item.
func sectionDepth(visit outlineVistData, name string) (depth int) {
	for i := 0; i < visit.Len(); i++ {
		if depth > 0 && visit.Title(i) != "" {
			depth++
		} else if depth == 0 && visit.Title(i) == name {
			depth++
		}
	}
	return depth
}

type outlineVisitor func(visit outlineVistData) blackfriday.WalkStatus

func walkOutline(node *blackfriday.Node, visitor outlineVisitor) {
	var o outlineWalker
	o.walk(node, visitor)
}

type outlineWalker struct {
	// TODO wants to extend blackfriday.nodeWalker rather than wrap blackfriday.Node.Walk
	t     []itemTime
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
	t        itemTime
	path     []*blackfriday.Node
	title    []string
}

func (wd outlineWalkerData) Entering() bool               { return wd.entering }
func (wd outlineWalkerData) Time() itemTime               { return wd.t }
func (wd outlineWalkerData) Len() int                     { return len(wd.path) }
func (wd outlineWalkerData) Node(i int) *blackfriday.Node { return wd.path[i] }
func (wd outlineWalkerData) Title(i int) string           { return wd.title[i] }

func (o *outlineWalker) enter(node *blackfriday.Node, visitor outlineVisitor) (status blackfriday.WalkStatus) {
	var t itemTime
	var skip bool
	t.loc = time.Local

	i := len(o.t)
	if j := i - 1; j >= 0 {
		t = o.t[j]
		skip = o.skip[j]
	}

	o.tmp.Reset()
	collectTitle(&o.tmp, node)
	title := t.Parse(o.tmp.String())

	o.t = append(o.t, t)
	o.path = append(o.path, node)
	o.title = append(o.title, title)
	o.skip = append(o.skip, skip)

	if !skip {
		status = visitor(outlineWalkerData{true, t, o.path, o.title})
		if status == blackfriday.SkipChildren {
			o.skip[i] = true
		}
	}
	return status
}

func (o *outlineWalker) exitTo(i int, visitor outlineVisitor) blackfriday.WalkStatus {
	defer func() {
		o.t = o.t[:i]
		o.path = o.path[:i]
		o.title = o.title[:i]
		o.skip = o.skip[:i]
	}()
	for j := len(o.path) - 1; j >= 0 && i <= j; j-- {
		if o.skip[j] {
			continue
		}
		if st := visitor(outlineWalkerData{false, o.t[j], o.path[:j+1], o.title}); st >= blackfriday.Terminate {
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

type markdownWriter struct {
	stack []renderContext
	renderContext
}

type renderContext struct {
	inLevel  int
	quote    byte
	nextItem int
}

func writeMarkdown(bw *bufWriter, node *blackfriday.Node) (err error) {
	var mw markdownWriter
	defer bw.finalWrite(&err)
	node.Walk(func(n *blackfriday.Node, entering bool) (status blackfriday.WalkStatus) {
		defer bw.walkFlush(&err, &status)

		switch n.Type {
		case blackfriday.Document:
			if !entering {
				mw.nl(bw, 1)
			}

		case blackfriday.Heading:
			mw.nl(bw, 2)
			if entering {
				for i := 0; i < n.Level; i++ {
					bw.buf.WriteByte('#')
				}
				bw.buf.WriteByte(' ')
			}

		case blackfriday.List:
			if mw.enter(entering) {
				if n.Parent.Type != blackfriday.Item {
					mw.nl(bw, 2)
				}
				mw.nextItem = 1
			}

		case blackfriday.Item:
			// TODO definition list support
			if mw.enter(entering) {
				mw.nl(bw, 1)
				mw.indent(bw)
				start := bw.buf.Len()
				if n.ListFlags&blackfriday.ListTypeOrdered != 0 {
					bw.buf.WriteString(strconv.Itoa(mw.nextItem))
					bw.buf.WriteByte(n.Delimiter)
				} else if n.BulletChar != 0 {
					bw.buf.WriteByte(n.BulletChar)
					bw.buf.WriteByte(' ')
				}
				mw.inLevel += bw.buf.Len() - start // TODO runewidth
			} else {
				mw.nextItem++
			}

		case blackfriday.BlockQuote:
			if mw.enter(entering) {
				mw.inLevel += 2
				mw.quote = '>'
			}

		case blackfriday.Paragraph:
			if n.Parent.Type != blackfriday.Item || n != n.Parent.FirstChild {
				mw.nl(bw, 2)
				if entering {
					mw.indent(bw)
				}
			}

		case blackfriday.HorizontalRule:
			mw.nl(bw, 2)
			if entering {
				mw.indent(bw)
				bw.buf.WriteString("---")
			}

		case blackfriday.Emph:
			bw.buf.WriteByte('*')
		case blackfriday.Strong:
			bw.buf.WriteString("**")
		case blackfriday.Del:
			bw.buf.WriteString("~~")

		case blackfriday.Link:
			// TODO footnote support
			if entering {
				bw.buf.WriteByte('[')
			} else {
				bw.buf.WriteString("](")
				bw.buf.Write(n.Destination)
				bw.buf.WriteByte(')')
			}

		case blackfriday.Image:
			if entering {
				bw.buf.WriteString("![")
			} else {
				bw.buf.WriteString("](")
				bw.buf.Write(n.Destination)
				bw.buf.WriteByte(')')
			}

		case blackfriday.Text:
			if entering {
				for b := n.Literal; len(b) > 0; {
					i := bytes.IndexByte(b, '\n')
					if i < 0 {
						bw.buf.Write(b)
						break
					}
					bw.buf.Write(b[:i])
					mw.nl(bw, 1)
					mw.indent(bw)
					b = b[i+1:]
				}
			}

		case blackfriday.CodeBlock:
			mw.nl(bw, 1)
			bw.buf.WriteString("```")
			bw.buf.Write(n.Info)
			mw.nl(bw, 1)
			bw.buf.Write(n.Literal)
			mw.nl(bw, 1)
			bw.buf.WriteString("```")

		case blackfriday.Code:
			bw.buf.WriteByte('`')
			bw.buf.Write(n.Literal)
			bw.buf.WriteByte('`')

		case blackfriday.Hardbreak:
			bw.buf.WriteByte('\\')
			fallthrough
		case blackfriday.Softbreak:
			mw.nl(bw, 1)

		case blackfriday.HTMLSpan, blackfriday.HTMLBlock:
			bw.buf.Write(n.Literal)

		// TODO table support

		default:
			if entering {
				mw.nl(bw, 1)
				mw.indent(bw)
				bw.buf.WriteString("{Unsupported")
			} else {
				bw.buf.WriteString("{/Unsupported")
			}
			bw.buf.WriteString(n.Type.String())
			bw.buf.WriteByte('}')
		}

		return blackfriday.GoToNext
	})
	return err
}

func (mw *markdownWriter) enter(entering bool) bool {
	if entering {
		mw.stack = append(mw.stack, mw.renderContext)
		return true
	}
	if i := len(mw.stack) - 1; i >= 0 {
		mw.renderContext = mw.stack[i]
		mw.stack = mw.stack[:i]
	} else {
		mw.renderContext = renderContext{}
	}
	return false
}

func (mw *markdownWriter) nl(bw *bufWriter, n int) {
	b := bw.buf.Bytes()
	if len(b) == 0 {
		return
	}

	m := 0
	for i := len(b) - 1; m < n && i >= 0 && b[i] == '\n'; i-- {
		m++
	}

	for ; m < n; m++ {
		bw.buf.WriteByte('\n')
	}
}

func (mw *markdownWriter) indent(bw *bufWriter) {
	i := 0
	n := mw.inLevel - 2
	for ; i < n; i++ {
		bw.buf.WriteByte(' ')
	}
	n += 2

	if mw.quote != 0 {
		bw.buf.WriteByte(mw.quote)
		if i++; i < n {
			bw.buf.WriteByte(' ')
			i++
		}
	}

	for ; i < n; i++ {
		bw.buf.WriteByte(' ')
	}
}

type bufWriter struct {
	out io.Writer
	buf bytes.Buffer
}

func (bw *bufWriter) maybeFlush() error {
	b := bw.shouldFlush()
	if len(b) == 0 {
		return nil
	}
	n, err := bw.out.Write(b)
	bw.buf.Next(n)
	return err
}

func (bw *bufWriter) shouldFlush() []byte {
	if bw.buf.Len() == 0 {
		return nil
	}
	b := bw.buf.Bytes()
	i := bytes.LastIndexByte(b, '\n')
	if i < 0 {
		return nil
	}
	return b[:i+1]
}

func (bw *bufWriter) finalWrite(err *error) {
	if _, werr := bw.buf.WriteTo(bw.out); *err == nil {
		*err = werr
	}
}

func (bw *bufWriter) walkFlush(err *error, status *blackfriday.WalkStatus) {
	if *err = bw.maybeFlush(); *err != nil {
		*status = blackfriday.Terminate
	}
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

func itemDate(year int, month time.Month, day int) itemTime {
	return itemTime{
		loc:   time.Local,
		level: timeLevelDay,
		year:  year,
		month: month,
		day:   day,
	}
}

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

func (t itemTime) Equal(other itemTime) bool {
	if other.level != t.level {
		return false
	}
	switch t.level {
	case timeLevelMinute:
		if other.minute != t.minute {
			return false
		}
		fallthrough
	case timeLevelHour:
		if other.loc.String() != t.loc.String() {
			return false
		}
		if other.hour != t.hour {
			return false
		}
		fallthrough
	case timeLevelDay:
		if other.day != t.day {
			return false
		}
		fallthrough
	case timeLevelMonth:
		if other.month != t.month {
			return false
		}
		fallthrough
	case timeLevelYear:
		if other.year != t.year {
			return false
		}
	}
	return true
}

// TODO func (t itemTime) Contains(other itemTime) bool

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
