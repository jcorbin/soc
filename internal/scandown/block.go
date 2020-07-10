package scandown

import (
	"bytes"
	"fmt"
	"io"
)

type BlockType int

const (
	noBlock BlockType = iota
	Document
	Heading
	Ruler
	SetextUnderline
	Blockquote
	List
	Item
	Paragraph
	Codefence
	Codeblock
	HTML // TODO
)

type Block struct {
	Type  BlockType
	Delim byte
	Width int
	// TODO Indent int
	// TODO export fields? or just semantic reader methods
}

type BlockStack struct {
	id     []int   // block id
	block  []Block // block kind (type, delim, width)
	offset []int   // within current scan window
	nextID int     // next block id
}

func (blocks *BlockStack) Offset() (n int) {
	// the Document node tracks total stream offset
	if len(blocks.block) > 0 && blocks.block[0].Type == Document {
		if docOffset := blocks.offset[0]; docOffset < 0 {
			n += -(docOffset + 1)
		}
	}
	// any final non-negative offsets is about to be pruned
	if len(blocks.offset) > 0 {
		if offset := blocks.offset[0]; offset >= 0 {
			n += offset
		}
	}
	return n
}

func (blocks *BlockStack) Head() (id int, b Block, open bool) {
	return blocks.Block(len(blocks.id) - 1)
}

func (blocks *BlockStack) Len() int { return len(blocks.id) }

func (blocks *BlockStack) Block(i int) (id int, b Block, open bool) {
	return blocks.id[i], blocks.block[i], blocks.offset[i] < 0
}

func (blocks *BlockStack) Scan(data []byte, atEOF bool) (advance int, token []byte, err error) {
	// advance decrements block offsets
	defer func() {
		if advance > 0 {
			for i := 0; i < len(blocks.offset); i++ {
				blocks.offset[i] -= advance
			}
		}
	}()

	{
		// (re)initialize when empty
		i := len(blocks.offset) - 1
		if i < 0 {
			blocks.nextID = 0
			blocks.truncate(0)
			blocks.open(Block{Document, 0, 0}, -1)
			i = 0
		}

		// prune the last closed block
		if end := blocks.offset[i]; end >= 0 {
			blocks.truncate(i)
			if i == 0 {
				blocks.offset = append(blocks.offset, 0)
			}
			advance = end
		}
	}

	var (
		line       []byte
		start, end = advance, -1
		sol        = start
		blanks     = 0
	)
scanLine:
	for {
		sol += len(line)
		if blanks == 0 {
			end = sol
		}
		line = data[sol:]
		if len(line) == 0 {
			if !atEOF {
				return advance, nil, nil
			}

			// close a block until Document
			if i := len(blocks.id) - 1; blocks.block[i].Type != Document {
				if isContainer(blocks.block[i].Type) {
					end = start
				}
				blocks.offset[i] = end
				return start, data[start:end], nil
			}

			// return a final text token containing trailing blank lines, nil
			if blanks > start {
				blocks.offset = append(blocks.offset, len(data))
				return start, data[start:], nil
			}

			// close Document
			blocks.offset[0] = end
			return end, nil, nil
		}

		if eol := bytes.IndexByte(line, '\n'); eol >= 0 {
			line = line[:eol+1]
		} else if !atEOF {
			return 0, nil, nil
		}

		// process the next non-empty line
		if len(bytes.TrimSpace(line)) > 0 {
			break
		}

		blanks++
		blocks.block[0].Width++
	}

	// matching non-empty lines through the open block stack
	var (
		tail    = line
		prior   Block
		matched = 0
	)
	for matched < len(blocks.id) {
		k := blocks.block[matched]
		if blanks > 0 && !mayContainBlanks(k.Type) {
			break
		}
		rest := continuesBlock(prior, k, tail)
		if rest == nil {
			break
		}
		tail = rest
		matched++
		prior = k
	}

	// either open a new block
	opened, cont := nextBlock(prior, tail)
	// or keep scanning lines
	if opened.Type == 0 {
		goto scanLine
	}

	// first close any unmatched blocks
	if matched < len(blocks.id) {
		blocks.offset[len(blocks.id)-1] = end
		return start, data[start:end], nil
	}

	// then return any blank line run token
	if blanks > 0 {
		// NOTE this passes an offset to prune, without actually opening a block
		blocks.offset = append(blocks.offset, sol)
		return start, data[start:sol], nil
	}

	// open the block, returning a container token, or continuing to scan leafs
	switch opened.Type {
	case SetextUnderline:
		end = sol + len(line)
		opened.Type = Heading
		opened.Width = 1
		if opened.Delim == '-' {
			opened.Width = 2
		}
		i := len(blocks.id) - 1
		blocks.block[i] = opened
		blocks.offset[i] = end
		return start, data[start:end], nil

	case Heading, Ruler:
		end = sol + len(line)
		blocks.open(opened, end)
		return start, data[start:end], nil

	case List:
		end = start
		blocks.open(opened, -1)
		return start, data[start:end], nil

	case Item, Blockquote:
		end = sol + len(line) - len(cont)
		blocks.open(opened, -1)
		blocks.offset = append(blocks.offset, end)
		return start, data[start:end], nil

	case Codeblock, Codefence, Paragraph:
		end = sol + len(line)
		blocks.open(opened, -1)
		goto scanLine

	default:
		return 0, nil, fmt.Errorf("unimplemented open %v", opened)
	}
}

func (blocks *BlockStack) open(b Block, end int) {
	i := len(blocks.id)
	blocks.id = append(blocks.id, blocks.nextID)
	blocks.block = append(blocks.block, b)
	if i < len(blocks.offset) {
		blocks.offset[i] = end
	} else {
		blocks.offset = append(blocks.offset, end)
	}
	blocks.nextID++
}

func (blocks *BlockStack) truncate(i int) {
	blocks.id = blocks.id[:i]
	blocks.block = blocks.block[:i]
	blocks.offset = blocks.offset[:i]
}

func mayContainBlanks(t BlockType) bool {
	switch t {
	case Document, Codefence, Codeblock:
		return true
	default:
		return false
	}
}

func isContainer(t BlockType) bool {
	switch t {
	case Document, List, Item, Blockquote:
		return true
	default:
		return false
	}
}

// TODO need to pass a `prior int` through to trimIndent so that tab may be
// discounted before sub-structure

type blockRecognizer func(prior Block, line []byte) (Block, []byte) // TODO abstract this for extensibility

func nextBlock(prior Block, line []byte) (Block, []byte) {
	for _, r := range []blockRecognizer{
		atxHeading,
		blockQuote,
		listItem,
		codeFence,
		thematicBreak,
		paragraph,
	} {
		if block, tail := r(prior, line); tail != nil {
			return block, tail
		}
	}
	return Block{}, nil
}

func continuesBlock(p, b Block, line []byte) []byte {
	switch b.Type {
	case Document:
		return line

	case Heading:
		return nil

	case Paragraph:
		// TODO unless interrupted?
		return line

	case List:
		// lists are continued if their items are
		return line

	case Item:
		// sufficient hanging indent
		if indent, tail := trimIndent(line, 0, b.Width); indent >= b.Width {
			return tail
		}
		// sibling item
		if i, cont := listItem(p, line); cont != nil && i.Delim == b.Delim {
			return line // preserve marker to open new block
		}
		return nil

	case Blockquote:
		if q, cont := blockQuote(p, line); cont != nil && q.Delim == b.Delim {
			return cont
		}
		return nil

	case Codefence:
		if indent, tail := trimIndent(line, 0, b.Width); indent >= b.Width {
			return tail
		}
		// TODO recognize closing fence
		return line

	case Codeblock:
		// TODO converge indent trim state with fence
		if indent, tail := trimIndent(line, 0, b.Width); indent >= b.Width {
			return tail
		}
		return nil

	default:
		return nil
	}
}

func atxHeading(prior Block, line []byte) (Block, []byte) {
	if isContainer(prior.Type) {
		_, tail := trimIndent(line, 0, 3)
		if head, level, tail := delimiter(tail, 6, '#'); tail != nil {
			_, tail = trimIndent(tail, 0, len(line))
			return Block{Heading, head, level}, tail
		}
	}
	return Block{}, nil
}

func blockQuote(prior Block, line []byte) (Block, []byte) {
	if isContainer(prior.Type) {
		_, tail := trimIndent(line, 0, 3)
		if quote, _, tail := delimiter(tail, 3, '>'); tail != nil {
			width := len(line) - len(tail)
			if in, tail := trimIndent(tail, 1, 3); in > 0 {
				return Block{Blockquote, quote, width}, tail
			}
		}
	}
	return Block{}, nil
}

func listItem(prior Block, line []byte) (Block, []byte) {
	// TODO recognize sibling vs sub by returning List? take branch away from opened.t switch?
	if isContainer(prior.Type) {
		_, tail := trimIndent(line, 0, 3)
		delim, _, cont := delimiter(tail, 3, 1, '-', '*', '+')
		if cont == nil {
			if delim, _, cont = ordinal(tail); delim != ')' && delim != '.' {
				cont = nil
			}
		}
		if cont != nil {
			width := len(line) - len(cont)
			if in, cont := trimIndent(cont, 1, 3); in > 0 {
				// TODO viva la sibling vs child
				if prior.Type != List {
					return Block{List, delim, width + in}, line
				}
				return Block{Item, delim, width + in}, cont
			}
		}
	}
	return Block{}, nil
}

func codeFence(prior Block, line []byte) (Block, []byte) {
	if isContainer(prior.Type) {
		in, tail := trimIndent(line, 0, 3)
		if delim, _, tail := fence(tail, 3, '`', '~'); tail != nil {
			// TODO remember fence width for close matching
			return Block{Codefence, delim, in}, tail
		}
	}
	return Block{}, nil
}

func thematicBreak(prior Block, line []byte) (Block, []byte) {
	if prior.Type == Paragraph {
		if delim, width, tail := ruler(line, '=', '-', '_', '*'); tail != nil {
			switch delim {
			case '=', '-':
				return Block{SetextUnderline, delim, width}, tail
			case '_', '*':
				return Block{Ruler, delim, width}, tail
			}
		}
	} else if isContainer(prior.Type) {
		if delim, width, tail := ruler(line, '-', '_', '*'); tail != nil {
			return Block{Ruler, delim, width}, tail
		}
	}
	return Block{}, nil
}

func paragraph(prior Block, line []byte) (Block, []byte) {
	if isContainer(prior.Type) {
		if len(bytes.TrimSpace(line)) == 0 {
			return Block{}, nil
		}
		in, tail := trimIndent(line, 0, 4)
		if in == 4 {
			return Block{Codeblock, 0, 0}, tail
		}
		return Block{Paragraph, 0, 0}, tail
	}
	return Block{}, nil
}

func delimiter(line []byte, maxWidth int, marks ...byte) (delim byte, width int, tail []byte) {
	if tail = line; len(tail) < 1 {
		return 0, 0, nil
	}
	if delim = tail[0]; !isByte(delim, marks...) {
		return 0, 0, nil
	}

	width++
	tail = tail[1:]
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

func ordinal(line []byte) (delim byte, width int, tail []byte) {
	tail = line
	for len(tail) > 0 {
		switch c := tail[0]; c {
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			width++
			tail = tail[1:]
			continue
		default:
			delim = c
			tail = tail[1:]
		}
		break
	}
	if width < 1 {
		return 0, 0, nil
	}
	return delim, width, tail
}

func fence(line []byte, min int, marks ...byte) (fence byte, width int, tail []byte) {
	// TODO allow indent?
	if len(line) < 1 {
		return 0, 0, nil
	}

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
	// TODO allow indent?
	if len(line) < 1 {
		return 0, 0, nil
	}
	if rule = line[0]; !isByte(rule, marks...) {
		return 0, 0, nil
	}
	for width++; width < len(line); width++ {
		switch line[width] {
		case rule, ' ', '\t':
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

func trimIndent(line []byte, prior, limit int) (n int, tail []byte) {
	for tail = line; n < limit && len(tail) > 0; tail = tail[1:] {
		if c := tail[0]; c == ' ' {
			n++
		} else if c == '\t' {
			if m := n + 4 - prior; m > limit {
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

func (b Block) Format(f fmt.State, _ rune) {
	if f.Flag('+') {
		switch b.Type {
		case Heading:
			fmt.Fprintf(f, "<%v delim=%q level=%v>", b.Type, b.Delim, b.Width)

		case Ruler:
			fmt.Fprintf(f, "<%v delim=%q width=%v>", b.Type, b.Delim, b.Width)

		case List:
			fmt.Fprintf(f, "<%v delim=%q>", b.Type, b.Delim)

		case Item, Codefence, Blockquote:
			fmt.Fprintf(f, "<%v delim=%q width=%v>", b.Type, b.Delim, b.Width)

		default:
			fmt.Fprintf(f, "<%v>", b.Type)
		}
	} else {
		switch b.Type {
		case Heading:
			fmt.Fprintf(f, "%v%v", b.Type, b.Width)
		case List:
			switch b.Delim {
			case '.', ')':
				io.WriteString(f, "OrderedList")
			default:
				io.WriteString(f, "List")
			}
		default:
			fmt.Fprint(f, b.Type)
		}
	}
}

func (blocks BlockStack) Format(f fmt.State, _ rune) {
	if f.Flag('+') {
		for i, offset := range blocks.offset {
			if i > 0 {
				io.WriteString(f, "\n")
			}
			if i >= len(blocks.id) {
				fmt.Fprintf(f, "%v. pending scan advance: %v", i+1, offset)
			} else if i == 0 {
				offset = -(offset + 1)
				fmt.Fprintf(f, "1. #%v %+v %v bytes scanned", blocks.id[i], blocks.block[i], offset)
			} else {
				offset = offset - blocks.offset[0]
				fmt.Fprintf(f, "%v. @%v #%v %+v", i+1, offset, blocks.id[i], blocks.block[i])
			}
		}
	} else {
		for i, offset := range blocks.offset {
			if i > 0 {
				io.WriteString(f, " ")
			}
			if i >= len(blocks.id) {
				fmt.Fprintf(f, "+%v", offset)
			} else if i == 0 && offset < 0 {
				fmt.Fprintf(f, "@%v %v#%v", -(offset + 1), blocks.block[i], blocks.id[i])
			} else if offset < 0 {
				fmt.Fprintf(f, "%v#%v", blocks.block[i], blocks.id[i])
			} else {
				fmt.Fprintf(f, "/%v#%v", blocks.block[i], blocks.id[i])
			}
		}
	}
}

func (t BlockType) Format(f fmt.State, _ rune) {
	switch t {
	case noBlock:
		io.WriteString(f, "None")
	case Document:
		io.WriteString(f, "Document")
	case Heading:
		io.WriteString(f, "Heading")
	case Paragraph:
		io.WriteString(f, "Paragraph")
	case SetextUnderline:
		io.WriteString(f, "SetextUnderline")
	case Ruler:
		io.WriteString(f, "Ruler")
	case List:
		io.WriteString(f, "List")
	case Item:
		io.WriteString(f, "Item")
	case Blockquote:
		io.WriteString(f, "Blockquote")
	case Codefence:
		io.WriteString(f, "Codefence")
	case Codeblock:
		io.WriteString(f, "Codeblock")
	case HTML:
		io.WriteString(f, "HTML")
	default:
		fmt.Fprintf(f, "InvalidBlock%v", int(t))
	}
}
