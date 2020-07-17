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
	width := b.Width
	switch b.Type {
	case Heading:
		fmt.Fprintf(f, "%v%v", b.Type, width)
		width = 0
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
	if f.Flag('+') {
		if d := b.Delim; d != 0 {
			fmt.Fprintf(f, " delim=%q", d)
		}
		if width != 0 {
			fmt.Fprintf(f, " width=%v", width)
		}
		if in := b.Indent; in != 0 {
			fmt.Fprintf(f, " indent=%v", in)
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
