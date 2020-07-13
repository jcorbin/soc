package scandown

// TODO evaluate handling of interior blanks
// TODO recognize closing code fence

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
	Blockquote
	List
	Item
	Paragraph
	Codefence
	Codeblock
	HTMLBlock // TODO
)

type Block struct {
	Type   BlockType
	Delim  byte
	Width  int
	Indent int
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
			blocks.open(Block{Document, 0, 0, 0}, -1)
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
	sol += len(line)
	if blanks == 0 {
		end = sol
	}
	line = data[sol:]
	if len(line) == 0 {
		if !atEOF {
			return advance, nil, nil
		}
		if i := len(blocks.id) - 1; blocks.block[i].Type != Document {
			// close a block until Document
			if isContainer(blocks.block[i].Type) {
				end = start
			}
			blocks.offset[i] = end
			return start, data[start:end], nil
		}
		if blanks > start {
			// return a final text token containing trailing blank lines, nil
			blocks.offset = append(blocks.offset, len(data))
			return start, data[start:], nil
		}
		// close Document
		blocks.offset[0] = sol
		return sol, nil, nil
	}

	// either find a new line, or read more data until EOF
	if eol := bytes.IndexByte(line, '\n'); eol >= 0 {
		line = line[:eol+1]
	} else if !atEOF {
		return 0, nil, nil
	}

	// TODO pushdown
	if len(bytes.TrimSpace(line)) <= 0 {
		blanks++
		blocks.block[0].Width++
		goto scanLine
	}

	// matching lines down the open block stack
	var (
		tail    = line
		matchi  = 0
		prior   Block
		opened  Block
		matched Block
	)
	for ; matchi < len(blocks.id) && opened.Type == 0 && len(tail) > 0; matchi, prior = matchi+1, matched {
		matched = blocks.block[matchi]
		if blanks > 0 && !mayContainBlanks(matched.Type) {
			break
		}

		switch matched.Type {
		case Document:
			continue

		case Paragraph:
			if _, cont := trimIndent(tail, 0, 3); len(cont) > 0 {
				if delim, _, cont := ruler(cont, '=', '-'); cont != nil {
					blocks.truncate(matchi)
					if delim == '=' {
						opened = Block{Heading, delim, 1, 0}
					} else {
						opened = Block{Heading, delim, 2, 0}
					}
					tail = cont
				}
				continue
			}

		case Codeblock:
			if indent, cont := trimIndent(tail, 0, matched.Indent); indent >= matched.Indent {
				tail = cont
				continue
			}

		case Codefence:
			_, tail = trimIndent(tail, 0, matched.Indent)
			continue

		case Blockquote:
			if _, cont := trimIndent(tail, 0, 3); len(cont) > 0 {
				if delim, _, cont := delimiter(cont, 3, '>'); delim != 0 {
					if post, _ := trimIndent(cont, 1, 3); post > 0 || len(cont) == 0 {
						tail = cont
						continue
					}
				}
			}

		case List:
			if indent, cont := trimIndent(tail, 0, matched.Indent); indent >= matched.Indent {
				var delim byte
				if _, inCont := trimIndent(tail, 0, 3); len(inCont) > 0 {
					delim, _, _ = listMarker(inCont)
				}
				if delim == 0 || delim == matched.Delim {
					tail = cont
					continue
				}
			}

		case Item:
			if indent, cont := trimIndent(tail, 0, matched.Width); indent >= matched.Width {
				tail = cont
				continue
			}
		}

		break
	}

	if opened.Type == 0 && len(tail) > 0 {
		opened = func() Block {
			// TODO need to honor prior delimiter, passing it to trimIndent to
			// discount any initial tab

			// TODO hoist
			if len(bytes.TrimSpace(tail)) == 0 {
				return Block{}
			}

			in, cont := trimIndent(tail, 0, 4)

			// Codeblock
			if in == 4 {
				return Block{Codeblock, 0, 0, in}
			}

			// blank line without enough indent to open a code block
			if len(cont) == 0 {
				return Block{}
			}

			// Codefence
			if delim, width, _ := fence(cont, 3, '`', '~'); delim != 0 {
				return Block{Codefence, delim, width, in}
			}

			// Ruler
			if delim, width, _ := ruler(cont, '-', '_', '*'); delim != 0 {
				return Block{Ruler, delim, width, in}
			}

			// Heading (atx marked)
			if delim, level, _ := delimiter(cont, 6, '#'); delim != 0 {
				return Block{Heading, delim, level, in}
			}

			// Blockquote
			if delim, width, cont := delimiter(cont, 3, '>'); delim != 0 {
				if post, _ := trimIndent(cont, 1, 3); post > 0 || len(cont) == 0 {
					return Block{Blockquote, delim, width + post, in}
				}
			}

			// List/Item
			if delim, width, _ := listMarker(cont); delim != 0 {
				if prior.Type != List {
					return Block{List, delim, width, in}
				}
				return Block{Item, delim, width, in}
			}

			// Paragraph
			return Block{Paragraph, 0, 0, in}
		}()

		if opened.Type == 0 {
			goto scanLine
		}
	}

	// close the head block if it didn't match
	if matchi < len(blocks.id) {
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
	case Heading, Ruler:
		end = sol + len(line)
		blocks.open(opened, end)
		return start, data[start:end], nil

	case List:
		end = start
		blocks.open(opened, -1)
		return start, data[start:end], nil

	case Item, Blockquote:
		end = sol + opened.Width
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

func listMarker(line []byte) (delim byte, width int, cont []byte) {
	delim, width, tail := delimiter(line, 3, '-', '*', '+')
	if delim == 0 {
		delim, width, tail = ordinal(line)
	}
	if delim != 0 {
		if in, cont := trimIndent(tail, 1, 3); in > 0 || len(cont) == 0 {
			return delim, width + in, cont
		}
	}
	return 0, 0, nil
}

func delimiter(line []byte, maxWidth int, marks ...byte) (delim byte, width int, tail []byte) {
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
	if delim == 0 || width < 1 {
		return 0, 0, nil
	}
	return delim, width, tail
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
	case HTMLBlock:
		io.WriteString(f, "HTMLBlock")
	default:
		fmt.Fprintf(f, "InvalidBlock%v", int(t))
	}
}
