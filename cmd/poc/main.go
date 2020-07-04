// Command poc is a proof-of-concept for Stream-Of-Consciousness ( SoC ).
package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/russross/blackfriday"
)

func main() {
	var (
		now    = time.Now()
		respTo = os.Stdout
		ui     userInterface

		mdExtensisons = 0 |
			blackfriday.NoIntraEmphasis |
			// blackfriday.DefinitionLists |
			// blackfriday.Tables |
			blackfriday.FencedCode |
			blackfriday.Autolink |
			blackfriday.Strikethrough |
			blackfriday.SpaceHeadings |
			blackfriday.HeadingIDs |
			blackfriday.BackslashLineBreak
	)
	ui.handle = handleUserRequest
	ui.store.from = os.Stdin
	ui.store.to = os.Stdout

	flag.Parse()
	ui.store.md = blackfriday.New(blackfriday.WithExtensions(mdExtensisons))

	// TODO detect stream file, prefer if stdin is tty; maybe only do stdio proc by flag
	if ui.store.to == os.Stdout && respTo == os.Stdout {
		respTo = os.Stderr // TODO in-situ response section/buffewr
	}

	if err := ui.serveArgs(now, flag.Args(), respTo); err != nil {
		log.Fatal(err)
	}
}

func handleUserRequest(st Stream, req *userRequest, resp *userResponse) error {
	if req.ScanArg() && req.Arg() == "outline" {
		writeOutlineInto(st.Root(), &resp.bufWriter)
	}
	return nil
}

type Stream interface {
	// TODO in the future this will lower to:
	//   In() io.Readder and
	//   Out() io.Writer
	// expecting the handler to implement a transfrom scan
	Root() *blackfriday.Node
	SetRoot(root *blackfriday.Node)

	Commit(message string, args ...interface{}) error
}

type userHandler func(st Stream, req *userRequest, resp *userResponse) error

type userInterface struct {
	store  streamStore
	handle userHandler
}

type streamStore struct {
	md *blackfriday.Markdown
	stream
	dirty bool

	// TODO support file path load and atomic write
	// TODO support change tracking
	// TODO support metadata checking
	from io.Reader
	bufWriter
}

type stream struct {
	doc *blackfriday.Node
}

type userRequest struct {
	now time.Time

	body        io.Reader
	bodyScanner *bufio.Scanner

	cmd        bytes.Reader
	cmdScanner *bufio.Scanner

	err error
}

type userResponse struct {
	bufWriter
}

func (ui *userInterface) serveArgs(now time.Time, args []string, respTo io.Writer) error {
	var req userRequest
	req.now = now
	req.body = bytes.NewReader(quotedArgs(args))
	return ui.serve(&req, respTo)
}

func (ui *userInterface) serve(req *userRequest, respTo io.Writer) error {
	if respTo == ui.store.to {
		return errors.New("in-situ response not supporteed") // TODO buffer then store
	}
	// TODO eliminate transactional load/save once we have atomic storage (rather than invalidated logging)
	return ui.store.with(func(st Stream) error {
		return req.serve(st, ui.handle, respTo)
	})
}

func (req *userRequest) serve(st Stream, handle userHandler, respTo io.Writer) (rerr error) {
	if err := req.rollover(st); err != nil {
		return err
	}

	defer func() {
		if rerr == nil {
			rerr = req.err
		}
	}()

	var resp userResponse
	resp.to = respTo
	defer func() {
		if ferr := resp.flush(); rerr == nil {
			rerr = ferr
		}
	}()

	return handle(st, req, &resp)
}

func (req *userRequest) Now() time.Time {
	return req.now
}

func (req *userRequest) ScanCommand() bool {
	if req.err != nil {
		return false
	}

	if req.bodyScanner == nil {
		if req.bodyScanner == nil && req.body != nil {
			// TODO use markdown scanning once we have it
			req.bodyScanner = bufio.NewScanner(req.body)
			req.bodyScanner.Split(bufio.ScanLines)
		}
	}
	req.cmd.Reset(nil)
	req.cmdScanner = nil

	if !req.bodyScanner.Scan() {
		req.err = req.bodyScanner.Err()
		return false
	}

	return true
}

func (req *userRequest) ScanArg() bool {
	if req.err != nil {
		return false
	}

	if req.cmdScanner == nil {
		if req.bodyScanner == nil && !req.ScanCommand() {
			return false
		}
		req.cmd.Reset(req.bodyScanner.Bytes())
		req.cmdScanner = bufio.NewScanner(&req.cmd)
		req.cmdScanner.Split(scanArgs)
	}

	if !req.cmdScanner.Scan() {
		req.err = req.cmdScanner.Err()
		return false
	}

	return true
}

func (req *userRequest) Command() string {
	if req.bodyScanner == nil {
		return ""
	}
	return req.bodyScanner.Text()
}

func (req *userRequest) Arg() string {
	if req.cmdScanner == nil {
		return ""
	}
	arg := req.cmdScanner.Text()
	if len(arg) > 2 && (arg[0] == '"' || arg[0] == '\'') {
		var sb strings.Builder
		sb.Grow(len(arg))
		for len(arg) > 0 {
			r, _, tail, err := strconv.UnquoteChar(arg, '"')
			if err != nil {
				sb.WriteString(arg)
				break
			}
			sb.WriteRune(r)
			arg = tail
		}
		arg = sb.String()
	}
	return arg
}

func (req *userRequest) Err() error {
	return req.err
}

func (st *streamStore) Root() *blackfriday.Node        { return st.doc }
func (st *streamStore) SetRoot(root *blackfriday.Node) { st.doc = root }

func (st *streamStore) with(under func(Stream) error) (err error) {
	if st.doc == nil {
		if err = st.load(); err != nil {
			return
		}
	}
	defer func() {
		if st.dirty {
			if serr := st.save(); err == nil {
				err = serr
			}
		}
	}()
	return under(st)
}

func (st *streamStore) load() error {
	b, err := ioutil.ReadAll(st.from)
	if err != nil {
		return err
	}
	st.dirty = false
	st.doc = st.md.Parse(b)
	return nil
}

func (st *streamStore) save() (err error) {
	defer func() {
		if err == nil {
			st.dirty = false
		}
	}()
	writeMarkdownInto(st.doc, &st.bufWriter)
	return st.flush()
}

func (st *streamStore) logf(message string, args ...interface{}) {
	if len(args) > 0 {
		message = fmt.Sprintf(message, args...)
	}

	var (
		logNode  *blackfriday.Node
		doneNode *blackfriday.Node
		firstDay *blackfriday.Node
	)
	walkOutline(st.Root(), func(visit outlineVistData) blackfriday.WalkStatus {
		if !visit.Entering() {
			return blackfriday.GoToNext
		}
		if sectionDepth(visit, "Log") == 1 {
			logNode = visit.Node(visit.Len() - 1)
			return blackfriday.Terminate
		}
		if isTemporal(visit) {
			if firstDay == nil && visit.Time().level == timeLevelDay {
				firstDay = visit.Node(visit.Len() - 1)
			}
		} else if sectionDepth(visit, "Done") == 1 {
			doneNode = visit.Node(visit.Len() - 1)
		}
		return blackfriday.GoToNext
	})

	if logNode == nil {
		logNode = blackfriday.NewNode(blackfriday.Heading)
		logNode.Level = 1
		text := blackfriday.NewNode(blackfriday.Text)
		text.Literal = []byte("SoC Log")
		logNode.AppendChild(text)
		if doneNode != nil {
			logNode.Level = doneNode.Level
			doneNode.InsertBefore(logNode)
		} else if firstDay != nil {
			logNode.Level = firstDay.Level + 1
			if firstDay.Next != nil {
				firstDay.Next.InsertBefore(logNode)
			} else {
				firstDay.Parent.AppendChild(logNode)
			}
		} else if st.doc.FirstChild != nil {
			st.doc.FirstChild.InsertBefore(logNode)
		} else {
			st.doc.AppendChild(logNode)
		}
	}

	var list *blackfriday.Node
	for n := logNode.Next; n != nil; n = n.Next {
		if n.Type == blackfriday.List {
			list = n
			break
		} else if n.Type == blackfriday.Heading {
			break
		}
	}
	if list == nil {
		list = blackfriday.NewNode(blackfriday.List)
		if logNode.Next != nil {
			logNode.Next.InsertBefore(list)
		} else {
			logNode.Parent.AppendChild(list)
		}
	}

	item := blackfriday.NewNode(blackfriday.Item)
	item.BulletChar = '+'
	text := blackfriday.NewNode(blackfriday.Text)
	text.Literal = []byte(message)
	item.AppendChild(text)
	list.AppendChild(item)
}

func (st *streamStore) Commit(message string, args ...interface{}) error {
	// TODO implement incremental save once we have file path
	// TODO integrate with git once we have it
	st.dirty = true
	st.logf(message, args...)
	return nil
}

func writeMarkdownInto(node *blackfriday.Node, bw *bufWriter) {
	var mw markdownWriter
	node.Walk(func(n *blackfriday.Node, entering bool) (status blackfriday.WalkStatus) {
		defer bw.walkFlush(&status)
		return mw.visitNode(&bw.buf, n, entering)
	})
}

func writeOutlineInto(node *blackfriday.Node, bw *bufWriter) {
	walkOutline(node, func(visit outlineVistData) (status blackfriday.WalkStatus) {
		defer bw.walkFlush(&status)
		if visit.Entering() {
			writeOutlineLine(&bw.buf, visit)
		}
		return blackfriday.GoToNext
	})
}

func (req *userRequest) rollover(st Stream) error {
	var (
		today       = itemDate(req.now.Date())
		firstDay    *blackfriday.Node
		todayNode   *blackfriday.Node
		horizonNode *blackfriday.Node
	)

	walkOutline(st.Root(), func(visit outlineVistData) blackfriday.WalkStatus {
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

		// TODO separate horizen rollover from today ensure; support rolling over into prior section

		firstDay.InsertBefore(todayNode)
		horizonNode.Next.InsertBefore(firstDay)

		// TODO promote/pull-down from upcoming

		if err := st.Commit("rollover"); err != nil {
			return err
		}
	}

	// TODO correct evacuation with pre-existing today section

	return nil
}

func writeOutlineLine(buf *bytes.Buffer, visit outlineVistData) {
	buf.WriteString(visit.Time().String())

	// build path with pure time components elided
	for i := 0; i < visit.Len(); i++ {
		title := visit.Title(i)
		if title == "" {
			continue
		}

		if buf.Len() > 0 {
			buf.WriteString(" ")
		}

		switch visit.Node(i).Type {
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

	buf.WriteByte('\n')
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

func (mw *markdownWriter) visitNode(buf *bytes.Buffer, node *blackfriday.Node, entering bool) blackfriday.WalkStatus {
	switch node.Type {
	case blackfriday.Document:
		if !entering {
			mw.nl(buf, 1)
		}

	case blackfriday.Heading:
		mw.nl(buf, 2)
		if entering {
			for i := 0; i < node.Level; i++ {
				buf.WriteByte('#')
			}
			buf.WriteByte(' ')
		}

	case blackfriday.List:
		if mw.enter(entering) {
			if node.Parent.Type != blackfriday.Item {
				mw.nl(buf, 2)
			}
			mw.nextItem = 1
		}

	case blackfriday.Item:
		// TODO definition list support
		if mw.enter(entering) {
			mw.nl(buf, 1)
			mw.indent(buf)
			start := buf.Len()
			if node.ListFlags&blackfriday.ListTypeOrdered != 0 {
				buf.WriteString(strconv.Itoa(mw.nextItem))
				buf.WriteByte(node.Delimiter)
			} else if node.BulletChar != 0 {
				buf.WriteByte(node.BulletChar)
				buf.WriteByte(' ')
			}
			mw.inLevel += buf.Len() - start // TODO runewidth
		} else {
			mw.nextItem++
		}

	case blackfriday.BlockQuote:
		if mw.enter(entering) {
			mw.inLevel += 2
			mw.quote = '>'
		}

	case blackfriday.Paragraph:
		if node.Parent.Type != blackfriday.Item || node != node.Parent.FirstChild {
			mw.nl(buf, 2)
			if entering {
				mw.indent(buf)
			}
		}

	case blackfriday.HorizontalRule:
		mw.nl(buf, 2)
		if entering {
			mw.indent(buf)
			buf.WriteString("---")
		}

	case blackfriday.Emph:
		buf.WriteByte('*')
	case blackfriday.Strong:
		buf.WriteString("**")
	case blackfriday.Del:
		buf.WriteString("~~")

	case blackfriday.Link:
		// TODO footnote support
		if entering {
			buf.WriteByte('[')
		} else {
			buf.WriteString("](")
			buf.Write(node.Destination)
			buf.WriteByte(')')
		}

	case blackfriday.Image:
		if entering {
			buf.WriteString("![")
		} else {
			buf.WriteString("](")
			buf.Write(node.Destination)
			buf.WriteByte(')')
		}

	case blackfriday.Text:
		if entering {
			for b := node.Literal; len(b) > 0; {
				i := bytes.IndexByte(b, '\n')
				if i < 0 {
					buf.Write(b)
					break
				}
				buf.Write(b[:i])
				mw.nl(buf, 1)
				mw.indent(buf)
				b = b[i+1:]
			}
		}

	case blackfriday.CodeBlock:
		mw.nl(buf, 1)
		buf.WriteString("```")
		buf.Write(node.Info)
		mw.nl(buf, 1)
		buf.Write(node.Literal)
		mw.nl(buf, 1)
		buf.WriteString("```")

	case blackfriday.Code:
		buf.WriteByte('`')
		buf.Write(node.Literal)
		buf.WriteByte('`')

	case blackfriday.Hardbreak:
		buf.WriteByte('\\')
		fallthrough
	case blackfriday.Softbreak:
		mw.nl(buf, 1)

	case blackfriday.HTMLSpan, blackfriday.HTMLBlock:
		buf.Write(node.Literal)

	// TODO table support

	default:
		if entering {
			mw.nl(buf, 1)
			mw.indent(buf)
			buf.WriteString("{Unsupported")
		} else {
			buf.WriteString("{/Unsupported")
		}
		buf.WriteString(node.Type.String())
		buf.WriteByte('}')
	}
	return blackfriday.GoToNext
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

func (mw *markdownWriter) nl(buf *bytes.Buffer, n int) {
	b := buf.Bytes()
	if len(b) == 0 {
		return
	}

	m := 0
	for i := len(b) - 1; m < n && i >= 0 && b[i] == '\n'; i-- {
		m++
	}

	for ; m < n; m++ {
		buf.WriteByte('\n')
	}
}

func (mw *markdownWriter) indent(buf *bytes.Buffer) {
	i := 0
	n := mw.inLevel - 2
	for ; i < n; i++ {
		buf.WriteByte(' ')
	}
	n += 2

	if mw.quote != 0 {
		buf.WriteByte(mw.quote)
		if i++; i < n {
			buf.WriteByte(' ')
			i++
		}
	}

	for ; i < n; i++ {
		buf.WriteByte(' ')
	}
}

type bufWriter struct {
	to  io.Writer
	buf bytes.Buffer
	err error
}

func (bw *bufWriter) maybeFlush() error {
	if bw.err != nil {
		return bw.err
	}
	b := bw.shouldFlush()
	if len(b) == 0 {
		return nil
	}
	n, err := bw.to.Write(b)
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

func (bw *bufWriter) flush() error {
	if _, werr := bw.buf.WriteTo(bw.to); bw.err == nil {
		bw.err = werr
	}
	return bw.err
}

func (bw *bufWriter) walkFlush(status *blackfriday.WalkStatus) {
	if bw.maybeFlush() != nil {
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

func quotedArgs(args []string) []byte {
	n := len(args)
	for _, arg := range args {
		n += 2 * len(arg)
	}
	b := make([]byte, 0, n)
	for _, arg := range args {
		if len(b) > 0 {
			b = append(b, ' ')
		}
		if strings.ContainsRune(arg, ' ') {
			b = strconv.AppendQuote(b, arg)
		} else {
			b = append(b, arg...)
		}
	}
	return b
}

func scanArgs(data []byte, atEOF bool) (advance int, token []byte, err error) {
	// Skip leading spaces.
	start := 0
	var r rune
	for width := 0; start < len(data); start += width {
		r, width = utf8.DecodeRune(data[start:])
		if !unicode.IsSpace(r) {
			break
		}
	}

	if r == '"' || r == '\'' {
		// Scan until end quote, skipping escaped quotoes.
		q := r
		esc := false
		for width, i := 0, start+1; i < len(data); i += width {
			r, width = utf8.DecodeRune(data[i:])
			if r == '\\' {
				esc = true
			} else if !esc && r == q {
				return i + width, data[start:i], nil
			} else {
				esc = false
			}
		}
	} else {
		// Scan until space.
		for width, i := 0, start; i < len(data); i += width {
			r, width = utf8.DecodeRune(data[i:])
			if unicode.IsSpace(r) {
				return i + width, data[start:i], nil
			}
		}
	}

	// If we're at EOF, we have a final, non-empty, non-terminated arg. Return it.
	if atEOF && len(data) > start {
		return len(data), data[start:], nil
	}
	// Request more data.
	return start, nil, nil
}
