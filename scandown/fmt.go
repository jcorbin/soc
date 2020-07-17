package scandown

import (
	"fmt"
	"io"
)

// Format writes a textual representation of the receiver, providing improved
// fmt.Printf display. Produces a multi-line verbose form when formatted with
// `%+v".
func (blocks BlockStack) Format(f fmt.State, _ rune) {
	if len(blocks.offset) == 0 {
		io.WriteString(f, "-- empty --")
		return
	}
	fmt.Fprintf(f, "@%v", -(blocks.offset[0] + 1))
	if f.Flag('+') {
		for i := 1; i < len(blocks.offset); i++ {
			offset := blocks.offset[i]
			io.WriteString(f, "\n")
			if i >= len(blocks.id) {
				fmt.Fprintf(f, "%v. pending scan advance: %v", i, offset)
			} else if offset < 0 {
				fmt.Fprintf(f, "%v. <%+v id=%v>", i, blocks.block[i], blocks.id[i])
			} else {
				fmt.Fprintf(f, "%v. </%+v id=%v>", i, blocks.block[i], blocks.id[i])
			}
		}
	} else {
		for i := 1; i < len(blocks.offset); i++ {
			offset := blocks.offset[i]
			io.WriteString(f, " ")
			if i >= len(blocks.id) {
				fmt.Fprintf(f, "+%v", offset)
			} else if offset < 0 {
				fmt.Fprintf(f, "%v#%v", blocks.block[i], blocks.id[i])
			} else {
				fmt.Fprintf(f, "/%v#%v", blocks.block[i], blocks.id[i])
			}
		}
	}
}

// Format writes a textual representation of the receiver, providing improved
// fmt.Printf display. Produces a verbose "Type attr=value" form when
// formatted with `%+v", a terse "Type" form otherwise.
func (b Block) Format(f fmt.State, _ rune) {
	if f.Flag('+') {
		switch b.Type {
		case Heading:
			fmt.Fprintf(f, "%v delim=%q level=%v", b.Type, b.Delim, b.Width)

		case Ruler:
			fmt.Fprintf(f, "%v delim=%q width=%v", b.Type, b.Delim, b.Width)

		case List:
			switch b.Delim {
			case '.', ')':
				fmt.Fprintf(f, "OrderedList delim=%q", b.Delim)
			default:
				fmt.Fprintf(f, "List delim=%q", b.Delim)
			}

		case Item, Codefence, Blockquote:
			fmt.Fprintf(f, "%v delim=%q width=%v", b.Type, b.Delim, b.Width)

		default:
			fmt.Fprintf(f, "%v", b.Type)
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

// Format writes a type string representing the receiver code.
func (t BlockType) Format(f fmt.State, _ rune) {
	switch t {
	case noBlock:
		io.WriteString(f, "None")
	case blank:
		io.WriteString(f, "Blank")
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
