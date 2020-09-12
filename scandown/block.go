package scandown

// TODO proper handling of virtual space, esp wrt tabs after {list,quote}Marker
// TODO CRLF handling probably needs improvement
// TODO BlockStack.Seek really needs a dedicated test
// TODO setext header content should have trailing space trimmed
// TODO support HTML block structure
// TODO support block structure extensions, like tables and definition lists
// TODO recognize reference link definitions... as blocks?
// TODO implement hard line breaks, currently stripped by content reader
// TODO should hard breaks in an atx heading continue it through the next line?
// TODO fragment leaf tokens: will place an upper limit on setext header sizes

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// BlockStack tracks state for Phase 1 parsing of Markdown block structure
// along the lines of the commonmark spec parsing strategy. It is logically a
// stack of open Block data, along with unique block IDs, and offsets
// within the scanned byte stream.
//
// Scan() implements a bufio.SplitFunc tokenizer, while Read() and Seek()
// implement and io.ReadSeeker over the last token's content bytes.
//
// It is not safe to use BlockStack from parallel goroutines.
type BlockStack struct {
	offset []int   // within current scan window
	block  []Block // block info
	id     []int   // block id
	nextID int     // next block id

	body  []byte // current block body; token retained by Scan
	read  []byte // remnant body bytes for Read()
	line  []byte // remnant body line bytes for Read()
	readn int64  // virtual body read offset ( see Seek )
}

// Block represents some piece of parsed Markdown block structure.
type Block struct {
	Type BlockType

	// Delim may contain a delimiter byte:
	// - Heading: '#', '=', or '-'
	// - Ruler: '-','_', or '*'
	// - Blockquote: '>''
	// - List/Item: '-', '*', '+', ')', or '.'
	// - Codefence: '`' or '~'
	Delim byte

	// Width may contain a mark width:
	// - Heading: the header level
	// - Ruler: counts how many rule bytes, including any spaces between
	// - Blockquote: width of the quote marker, including any following spaces
	// - Item: width of the list item marker, including any following spaces
	// - Codefence: counts how many fence Delim bytes were used
	Width int

	// Indent tracks how far the block was indented, uses:
	// - Item: to recognize hanging indent (along with Width) and trim content
	// - Codefence: to trim following content
	// - Codeblock: to recognize block end, and trim content
	Indent int
}

// BlockType is to determine the semantic meaning of a Block.
type BlockType int

// BlockType constants for the core commonmark structures.
const (
	noBlock BlockType = iota // 0 value should never be seen by user
	Blank
	Document
	Heading
	Ruler
	Blockquote
	List
	Item
	Paragraph
	Codefence
	Codeblock
	HTMLBlock
)

// Scan implements a bufio.SplitFunc that tokenizes Markdown block structure.
//
// It consumes lines (explicitly terminated) from the data buffer, matching
// against and updating the receiver block stack state. If atEOF is true, it
// also consumes a final unterminated line, and then closes any open blocks.
//
// Any non-nil error returned SHOULD cause the caller to halt its scan loop,
// and not not call Scan again.
//
// The returned advance is how many prefix data bytes MUST be discarded before
// the next Scan. This prefix MAY ( but does not currently ) include any token
// bytes.
//
// The returned token MAY be a window within data, so must not be retained
// across calls to Scan, and becomes invalid once the caller advance-s data.
//
// Finally, Scan() resets Read() and Seek() state to extract content from the
// returned token bytes.
//
// Example usage:
// 	var blocks scandown.BlockStack
// 	sc := bufio.NewScanner(os.Stdin)
// 	sc.Split(blocks.Scan)
// 	for sc.Scan() {
// 		content, _ := ioutil.ReadAll(&blocks)
// 		fmt.Printf("scanned %v %q\n", blocks, content)
// 	}
func (blocks *BlockStack) Scan(data []byte, atEOF bool) (advance int, token []byte, err error) {
	// initialize body Read() state on the way out
	defer func() {
		blocks.body = token
		blocks.read = token
		blocks.line = nil
		blocks.readn = 0
	}()

	// decrement block offsets by final advance
	defer func() {
		if advance > 0 {
			for i := 0; i < len(blocks.offset); i++ {
				blocks.offset[i] -= advance
			}
		}
	}()

	for i := len(blocks.offset) - 1; ; i-- {
		// (re)initialize empty blocks
		if i < 0 {
			blocks.offset = append(blocks.offset[:0], -1)
			blocks.block = append(blocks.block[:0], Block{Document, 0, 0, 0})
			blocks.id = append(blocks.id[:0], 0)
			blocks.nextID = 1
			break
		}

		// pop all blocks ended by a prior Scan
		end := blocks.offset[i]
		if end < 0 {
			i++
			blocks.offset = blocks.offset[:i]
			blocks.block = blocks.block[:i]
			blocks.id = blocks.id[:i]
			break
		}

		// advance past any prior consumed bytes
		if end > advance {
			advance = end
		}
	}

	// line consumption loop state
	var (
		start, end = advance, -1 // proto-token offsets withing the data buffer
		sol        = start       // offset of the current line being consumed
		line       []byte        // its bytes within the data buffer
	)
	defer func() {
		// construct token when returning nil-error and non-negative end
		if err == nil && end >= start {
			token = data[start:end]
		}
	}()

	// line consumption loop:
	// - scans the next token of block structure
	// - a container block open/close token will be empty but non-nil
	// - a leaf block token spans, potentially many, lines
	// - an interstitial space token is attributed to the deepest container
	//   block possible, between any sibling leaves
consumeLine: // labeled to clarify `continue` sites, some hundreds of lines hence
	for {
		// start out a(nother) line after after the last one
		sol += len(line)
		line = data[sol:]

		// scan all bytes until newline or EOF
		if eol := bytes.IndexByte(line, '\n'); eol >= 0 {
			line = line[:eol+1]
		} else if !atEOF {
			return
		} else if len(line) == 0 {
			if i := len(blocks.offset) - 1; i == 0 {
				blocks.offset = append(blocks.offset, sol)
			} else {
				end = sol
				blocks.offset[i] = end
			}
			return
		}

		// consume line bytes, matching prior blocks
		var (
			tail   = trimNewline(line)
			priori int
			prior  Block
		)
	matchPrior:
		for priori = 0; priori < len(blocks.block); priori++ {
			switch prior = blocks.block[priori]; prior.Type {
			case Document:
				// any line continues the document

			case Blank:
				// blank line runs are continued only by blank lines short
				// enough to not open an indented codeblock
				if indent, cont := trimIndent(tail, 0, 4); indent == 4 || len(cont) > 0 {
					break matchPrior
				}

			case Paragraph:
				// must check for all other block open markers before deciding
				// if a paragraph has been continued or terminated
				break matchPrior

			case Codefence:
				// fenced code blocks are continued until their ending fence
				// ( or end of container, by failing a prior round of this loop )
				_, tail = trimIndent(tail, 0, prior.Indent)
				if _, cont := trimIndent(tail, 0, 3); len(cont) > 0 {
					delim, _, cont := fence(cont, prior.Width, prior.Delim)
					if delim != 0 && len(bytes.Trim(cont, " ")) == 0 {
						end = sol + len(line)
						break matchPrior
					}
				}

			case Codeblock:
				// indented codeblocks are continued by sufficient indent and blank lines
				if indent, cont := trimIndent(tail, 0, prior.Indent); indent < prior.Indent && len(bytes.TrimSpace(cont)) != 0 {
					break matchPrior
				} else {
					tail = cont
				}

			case Blockquote:
				// block quotes are continued when opened and by additional quote markers
				if offset := sol + blocks.offset[priori]; offset == -1 {
					tail = tail[prior.Width:] // newly opened
				} else if _, cont := trimIndent(tail, 0, 3); len(cont) == 0 {
					break matchPrior
				} else if delim, _, cont := quoteMarker(cont); delim == 0 {
					break matchPrior
				} else {
					tail = cont
				}

			case List:
				// lists are continued, after open, by sibling items or terminated by a differing delimiter
				// otherwise continuation is handled by the next ( Item ) stack entry
				if hi := prior.Indent + prior.Width; hi > 0 {
					if in, cont := trimIndent(tail, 0, hi); in < hi && len(cont) > 0 {
						if delim, _, _ := listMarker(cont); delim != 0 {
							if delim != prior.Delim {
								break matchPrior
							}
							if nexti := priori + 1; nexti < len(blocks.offset) {
								if offset := sol + blocks.offset[nexti]; offset != -1 {
									priori = nexti
									break matchPrior
								}
							}
						}
					}
				}

			case Item:
				// list items are continued when opened and by sufficient indent
				hi := prior.Indent + prior.Width
				if offset := sol + blocks.offset[priori]; offset == -1 {
					tail = tail[hi:] // newly opened
				} else if indent, cont := trimIndent(tail, 0, hi); len(cont) > 0 && indent < hi {
					break matchPrior
				} else {
					tail = cont
				}

			default:
				err = fmt.Errorf("unimplemented match prior[%v]: %v", priori, prior)
				return
			}
		}

		// recognize remaining line bytes, finalizing any paragraph continuation match from above
		// - may terminate blocks suffix unmatched above
		// - may open under prior container
		// - may interrupt prior paragraph
		// - may transform prior paragraph into a setext header
		// - may terminate a paragraph on blank line
		// - may open a paragraph or blank leaf
		// - may lazily continue a head paragraph, despite unmatched priors
		var opened Block
		if priori < len(blocks.id) || isContainer(prior.Type) {
			// TODO honor prior delimiter, passing non-0 prior discount to trimIndent
			indent, cont := trimIndent(tail, 0, 4)
			if prior.Type != Paragraph && indent == 4 {
				opened = Block{Codeblock, 0, 0, indent}
			} else if len(bytes.TrimSpace(cont)) == 0 {
				opened = Block{Blank, 0, 0, 0}
			} else if delim, _, _ := ruler(cont, '=', '-'); prior.Type == Paragraph && delim != 0 {
				opened = Block{Heading, delim, 1, indent}
				if delim == '-' {
					opened.Width = 2
				}
				blocks.offset = blocks.offset[:priori]
				blocks.block = blocks.block[:priori]
				blocks.id = blocks.id[:priori]
			} else if delim, width, _ := fence(cont, 3, '`', '~'); delim != 0 {
				opened = Block{Codefence, delim, width, indent}
			} else if delim, width, _ := ruler(cont, '-', '_', '*'); delim != 0 {
				opened = Block{Ruler, delim, width, indent}
			} else if delim, level, _ := delimiter(cont, 6, '#'); delim != 0 {
				opened = Block{Heading, delim, level, indent}
			} else if delim, width, _ := quoteMarker(cont); delim != 0 {
				opened = Block{Blockquote, delim, width, indent}
			} else if delim, width, _ := listMarker(cont); delim != 0 {
				if prior.Type != List {
					opened = Block{List, delim, 0, 0}
				} else {
					opened = Block{Item, delim, width, indent}
				}
			} else if prior.Type == Paragraph {
				priori++
			} else if n := len(blocks.id); blocks.block[n-1].Type == Paragraph {
				priori = n
			} else {
				opened = Block{Paragraph, 0, 0, indent}
			}
		}

		// TODO seems a bit hacky
		if priori == len(blocks.id) && prior.Type == List && opened.Type != Item && opened.Type != Blank {
			priori--
		}

		// close the head block if unmatched
		if priori < len(blocks.id) {
			if end < start {
				end = sol
			}
			blocks.offset[len(blocks.offset)-1] = end
			return
		}

		// continue scan until a block open
		if opened.Type == 0 {
			// TODO leaf token fragmentation (to limit the buffer liability for large leaves)
			continue consumeLine
		}

		// finally ready to open a block, returning any container open token
		if opened.Type == Item {
			// update parent list indent
			prior.Width = opened.Width
			prior.Indent = opened.Indent
			blocks.block[len(blocks.block)-1] = prior
		}
		if i := len(blocks.id); i < len(blocks.offset) {
			blocks.offset[i] = end
		} else {
			blocks.offset = append(blocks.offset, end)
		}
		blocks.block = append(blocks.block, opened)
		blocks.id = append(blocks.id, blocks.nextID)
		blocks.nextID++

		switch opened.Type {
		case Heading, Ruler:
			// these end on the line detected
			end = sol + len(line)
			blocks.offset[len(blocks.offset)-1] = end
			return

		case List, Item, Blockquote:
			// these emit an empty token on open
			end = start
			return
		}

		// continue consumeLine // implicit since this is loop tail
	}
}

// Read implements io.Reader over the content bytes of the last scanned token.
//
// See Scan for details and an example.
func (blocks *BlockStack) Read(p []byte) (n int, err error) {
	blocks.read, blocks.line, n = copyBlockContent(blocks.block, blocks.read, blocks.line, p)
	if len(blocks.read) == 0 {
		err = io.EOF
	}
	blocks.readn += int64(n)
	return n, err
}

// Seek implements io.Seeker over the content bytes of the last scanned token.
//
// See Scan for details and an example.
func (blocks *BlockStack) Seek(offset int64, whence int) (int64, error) {
	switch whence {

	case io.SeekCurrent:
		if offset == 0 {
			return blocks.readn, nil
		}

	case io.SeekEnd:
		blocks.skip(int64(len(blocks.line) + len(blocks.read)))
		offset = -offset

	case io.SeekStart:
		// restarts are easy
		if offset == 0 {
			blocks.read = blocks.body
			blocks.line = nil
			blocks.readn = 0
			return 0, nil
		}
		offset -= blocks.readn

	default:
		return 0, errors.New("invalid seek whence") // TODO standard error?
	}

	// startover to go back
	if offset < 0 {
		to := offset + blocks.readn
		if to < 0 {
			return 0, errors.New("seek offset out of range") // TODO standard error?
		}
		blocks.read = blocks.body
		blocks.line = nil
		offset = to
	}

	// no change
	if offset == 0 {
		return blocks.readn, nil
	}

	// skip forward
	if blocks.skip(offset) != offset {
		return 0, errors.New("seek offset out of range") // TODO standard error?
	}
	return offset, nil
}

// TODO may be a hint for a better receiver type scope wrt []Block functions below
func (blocks *BlockStack) skip(max int64) (skipped int64) {
	blocks.read, blocks.line, skipped = skipBlockContent(blocks.block, blocks.read, blocks.line, max)
	blocks.readn += skipped
	return skipped
}

// TODO maybe receive on some type T []Block
func copyBlockContent(blocks []Block, token, line, dst []byte) (remToken, remLine []byte, n int) {
	// TODO try to unify with skipBlockContent
	head := blocks[len(blocks)-1]
	if head.Type == Blank {
		return nil, nil, 0
	}
	for {
		if len(line) > 0 {
			cn := copy(dst, line)
			line = line[cn:]
			switch head.Type {
			case Heading, Paragraph:
				cn = coalesceSpace(dst, dst[:cn])
				if pn := cn - 1; len(token) == 0 && pn >= 0 && dst[pn] == ' ' {
					// trim a trailing space
					cn = pn
				}
			}
			n += cn
			dst = dst[cn:]
		}
		if len(token) == 0 || len(dst) == 0 {
			break
		}
		token, line = nextBlockLine(blocks, token)
	}
	return token, line, n
}

// TODO maybe receive on some type T []Block
func skipBlockContent(blocks []Block, token, line []byte, skip int64) (remToken, remLine []byte, n int64) {
	// TODO try to unify with copyBlockContent
	head := blocks[len(blocks)-1]
	if head.Type == Blank {
		return nil, nil, 0
	}
	for {
		if ln := int64(len(line)); ln > 0 {
			if ln > skip {
				ln = skip
			}
			nextLine := line[ln:]
			switch head.Type {
			case Heading, Paragraph:
				sn := coalesceSpace(nil, line[:ln])
				if pn := ln - 1; len(token) == 0 && pn >= 0 && line[pn] == ' ' {
					// trim a trailing space
					sn--
				}
			}
			n += ln
			skip -= ln
			line = nextLine
		}
		if len(token) == 0 || skip <= 0 {
			break
		}
		token, line = nextBlockLine(blocks, token)
	}
	return token, line, n
}

// TODO maybe receive on some type T []Block
func nextBlockLine(blocks []Block, token []byte) (remToken, nextLine []byte) {
	line := token
	if eol := bytes.IndexByte(line, '\n'); eol < 0 {
		token = nil
	} else {
		eol++
		line = line[:eol]
		token = token[eol:]
	}
	return token, trimBlockLine(blocks, line)
}

// TODO maybe receive on some type T []Block
func trimBlockLine(blocks []Block, line []byte) []byte {
	for _, b := range blocks {
		switch b.Type {

		case Blockquote:
			_, line = trimIndent(line, 0, 3)
			_, _, line = delimiter(line, 1, b.Delim)
			_, line = trimIndent(line, 1, 1)

		case Item:
			hi := b.Indent + b.Width
			var in int
			in, line = trimIndent(line, 0, hi)
			if in < hi {
				// TODO do we need to receive state to enforce only first-line delimiter?
				tail := line
				d := b.Delim
				switch d {
				case ')', '.':
					_, tail = ordinal(tail)
				}
				rd, _, tail := delimiter(tail, 1, d)
				_, tail = trimIndent(tail, 1, 1)
				if rd != 0 {
					line = tail
				}
			}

		case Ruler:
			if d, _, _ := ruler(line, b.Delim); d != 0 {
				return nil
			}

		case Heading:
			switch d := b.Delim; d {
			case '#':
				_, _, line = delimiter(line, b.Width, '#')
			case '=', '-':
				// TODO should we enforce last line only if we receive that state?
				if rd, _, _ := ruler(line, d); rd != 0 {
					return nil
				}
			}
			fallthrough

		case Blank, Paragraph:
			_, line = trimIndent(line, 0, len(line))

		case Codefence:
			// TODO should we leverage first/last line knowledge if we receive it?
			_, line = trimIndent(line, 0, b.Indent)
			if _, cont := trimIndent(line, 0, 3); len(cont) > 0 {
				if delim, _, _ := fence(cont, b.Width, b.Delim); delim != 0 {
					return nil
				}
			}

		case Codeblock:
			_, line = trimIndent(line, 0, b.Indent)

		}
	}
	return line
}

func coalesceSpace(dst, src []byte) (n int) {
	between := true
	for _, c := range src {
		switch c {
		case '\r':
		case '\t', '\n':
			c = ' '
			fallthrough
		case ' ':
			if between {
				continue
			}
			between = true
		default:
			between = false
		}
		if dst != nil {
			dst[n] = c
		}
		n++
	}
	return n
}

// Offset returns the current scanned stream offset, where the currently
// scanned token starts.
func (blocks *BlockStack) Offset() (n int64) {
	// the Document node tracks total stream offset
	if len(blocks.block) > 0 && blocks.block[0].Type == Document {
		if docOffset := blocks.offset[0]; docOffset < 0 {
			n += -(int64(docOffset) + 1)
		}
	}
	// any final non-negative offsets is about to be pruned
	if len(blocks.offset) > 0 {
		if offset := blocks.offset[0]; offset >= 0 {
			n += int64(offset)
		}
	}
	return n
}

// Reset clears all receiver state, preparing it to scan a new stream.
func (blocks *BlockStack) Reset() {
	blocks.offset = blocks.offset[:0]
	blocks.block = blocks.block[:0]
	blocks.id = blocks.id[:0]
	blocks.nextID = 0
	blocks.body = blocks.body[:0]
	blocks.read = blocks.read[:0]
	blocks.line = blocks.line[:0]
	blocks.readn = 0
}

// Len returns how many blocks are currently on the stack.
func (blocks *BlockStack) Len() int {
	return len(blocks.id)
}

// ID returns a unique id number for a single block on the stack.
func (blocks *BlockStack) ID(i int) int {
	return blocks.id[i]
}

// Block returns the Block definition for a single block on the stack, and
// whether the block is still open (true), or has been closed (false). Panics
// if i >= Len().
func (blocks *BlockStack) Block(i int) (b Block, open bool) {
	return blocks.block[i], blocks.offset[i] < 0
}

// Head is a convenience for ID(Len()-1).
func (blocks *BlockStack) HeadID() (id int) {
	return blocks.ID(len(blocks.id) - 1)
}

// Head is a convenience for Block(Len()-1).
func (blocks *BlockStack) Head() (b Block, open bool) {
	return blocks.Block(len(blocks.id) - 1)
}

func isContainer(t BlockType) bool {
	switch t {
	case Document, List, Item, Blockquote:
		return true
	default:
		return false
	}
}

// ParseMark parses a single block mark from the given line bytes, optionally
// skipping past a fixed amount of prior expected indent.
// Returns any parsed block mark and the line trailer bytes, or the zero Block
// and all line bytes if no mark can be parsed.
func ParseMark(prior int, line []byte) (Block, []byte) {
	if len(line) > 0 {
		if indent, cont := trimIndent(line, 0, prior); len(cont) > 0 {
			if indent < prior {
				return Block{}, line
			}
			if delim, width, cc := fence(cont, 3, '`', '~'); delim != 0 {
				return Block{Codefence, delim, width, indent}, cc
			}
			if delim, width, _ := ruler(cont, '-', '_', '*'); delim != 0 {
				return Block{Ruler, delim, width, indent}, nil
			}
			if delim, width, qc := quoteMarker(cont); delim != 0 {
				return Block{Blockquote, delim, width, indent}, qc
			}
			if delim, width, lc := listMarker(cont); delim != 0 {
				return Block{Item, delim, width, indent}, lc
			}
		}
	}
	return Block{}, line
}

// MarkString returns a string prefix necessary to open the block.
func (block Block) MarkString() string {
	var sb strings.Builder
	block.justWriteMark(&sb)
	return sb.String()
}

func (block Block) AppendMark(into []byte) []byte {
	var aw appendWriter
	aw.buf = into
	block.justWriteMark(&aw)
	return aw.buf
}

func (block Block) WriteMarkInto(into io.Writer) (n int64, err error) {
	if sw, ok := into.(io.StringWriter); ok {
		return block.writeMark(sw)
	}
	var buf bytes.Buffer
	buf.Grow(64)
	if n, err = block.writeMark(&buf); err != nil {
		return 0, err
	}
	return buf.WriteTo(into)
}

type resetStringWriter interface {
	io.StringWriter
	Reset()
}

func (block Block) justWriteMark(into resetStringWriter) {
	if _, err := block.writeMark(into); err != nil {
		into.Reset()
		into.WriteString("!ERROR(")
		into.WriteString(err.Error())
		into.WriteString(")")
	}
}

func (block Block) writeMark(into io.StringWriter) (n int64, err error) {
	writeString := func(s string) {
		if err == nil {
			var m int
			m, err = into.WriteString(s)
			n += int64(m)
		}
	}

	in := strings.Repeat(" ", block.Indent)
	switch block.Type {
	case noBlock, Blank, Document, List, Paragraph, HTMLBlock:
		writeString(in)

	case Heading:
		switch d := block.Delim; d {
		case '#':
			writeString(in)
			writeString(strings.Repeat(string(d), block.Width))
			writeString(" ")
		case '=', '-':
			writeString(in)
			writeString(strings.Repeat(string(d), 3))
		default:
			return 0, fmt.Errorf("invalid Heading delim %q", d)
		}

	case Ruler:
		switch d := block.Delim; d {
		case '-', '_', '*':
			writeString(in)
			writeString(strings.Repeat(string(d), block.Width))
		default:
			return 0, fmt.Errorf("invalid Ruler delim %q", d)
		}

	case Codefence:
		switch d := block.Delim; d {
		case '`', '~':
			writeString(in)
			writeString(strings.Repeat(string(d), block.Width))
			writeString(" ")
		default:
			return 0, fmt.Errorf("invalid Codefence delim %q", d)
		}

	case Blockquote:
		d := block.Delim
		if d != '>' {
			return 0, fmt.Errorf("invalid BlockQuote delim %q", d)
		}
		writeString(in)
		writeString(string(d))
		writeString(" ")

	case Item:
		switch d := block.Delim; d {
		case '-', '*':
			writeString(in)
			writeString(string(d))
			writeString(" ")
		case '.', ')':
			writeString(in)
			writeString(strconv.FormatInt(int64(block.Width), 10))
			writeString(string(d))
			writeString(" ")
		default:
			return 0, fmt.Errorf("invalid Item delim %q", d)
		}

	default:
		return 0, fmt.Errorf("invalid block type %v", block.Type)
	}
	return n, err
}

func quoteMarker(line []byte) (delim byte, width int, cont []byte) {
	if delim, width, tail := delimiter(line, 3, '>'); delim != 0 {
		// TODO this wants to be able to consume a single virtual space from a tab, passing any remainder
		if in, cont := trimIndent(tail, 1, 1); in > 0 || len(cont) == 0 {
			return delim, width + in, cont
		}
	}
	return 0, 0, nil
}

func listMarker(line []byte) (delim byte, width int, cont []byte) {
	delim, width, tail := delimiter(line, 1, '-', '*', '+')
	if delim == 0 {
		if width, tail = ordinal(line); len(tail) > 0 {
			var dw int
			delim, dw, tail = delimiter(tail, 1, '.', ')')
			width += dw
		}
	}
	if delim != 0 {
		// TODO this wants to be able to consume a single virtual space from a tab, passing any remainder
		if in, cont := trimIndent(tail, 1, 1); in > 0 || len(cont) == 0 {
			return delim, width + in, cont
		}
	}
	return 0, 0, nil
}

func delimiter(line []byte, maxWidth int, marks ...byte) (delim byte, width int, tail []byte) {
	if delim = line[0]; !isByte(delim, marks...) {
		return 0, 0, nil
	}

	width++
	tail = line[1:]
	for {
		if len(tail) == 0 {
			return delim, width, tail
		}
		switch tail[0] {
		case delim:
			if width++; width > maxWidth {
				return 0, 0, nil
			}
			tail = tail[1:]
		case ' ', '\t':
			return delim, width, tail
		default:
			return 0, 0, nil
		}
	}
}

func ordinal(line []byte) (width int, tail []byte) {
	tail = line
	for len(tail) > 0 {
		switch c := tail[0]; c {
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			width++
			tail = tail[1:]
			continue
		}
		break
	}
	if width < 1 || width > 9 {
		return 0, nil
	}
	return width, tail
}

func fence(line []byte, min int, marks ...byte) (fence byte, width int, tail []byte) {
	if fence = line[0]; !isByte(fence, marks...) {
		return 0, 0, nil
	}
	width++

	for ; width < len(line); width++ {
		if line[width] != fence {
			break
		}
	}

	if width < min {
		return 0, 0, nil
	}

	return fence, width, line[width:]
}

func ruler(line []byte, marks ...byte) (rule byte, width int, tail []byte) {
	// TODO min width?
	if rule = line[0]; !isByte(rule, marks...) {
		return 0, 0, nil
	}
	for width++; width < len(line); width++ {
		switch line[width] {
		case rule, ' ', '\t':
		case '\n':
			width--
			return rule, width, line[width:]
		default:
			return 0, 0, nil
		}
	}
	return rule, width, line[width:]
}

func isByte(b byte, any ...byte) bool {
	for _, ab := range any {
		if b == ab {
			return true
		}
	}
	return false
}

func trimNewline(line []byte) []byte {
	i := len(line) - 1
	if i < 0 {
		return line
	}
	for i >= 0 {
		switch line[i] {
		case '\r', '\n':
			i--
		default:
			return line[:i+1]
		}
	}
	return line[:0]
}

func trimIndent(line []byte, prior, limit int) (n int, tail []byte) {
	for tail = line; n < limit && len(tail) > 0; tail = tail[1:] {
		if c := tail[0]; c == ' ' {
			n++
		} else if c == '\t' {
			if m := n + 4 - prior; m > limit {
				// TODO ability to split the tab, and return "tail with remaining indent"
				return n, tail
			} else if m == limit {
				return m, tail
			}
			prior = 0
		} else {
			break
		}
	}
	return n, tail
}

type appendWriter struct {
	buf  []byte
	orig []byte
}

func (aw *appendWriter) Reset() {
	if buf := aw.orig; buf != nil {
		aw.buf = buf
	}
}

func (aw *appendWriter) Write(p []byte) (n int, err error) {
	if aw.orig == nil {
		aw.orig = aw.buf
	}
	aw.buf = append(aw.buf, p...)
	return len(p), nil
}

func (aw *appendWriter) WriteString(s string) (n int, err error) {
	if aw.orig == nil {
		aw.orig = aw.buf
	}
	aw.buf = append(aw.buf, s...)
	return len(s), nil
}
