package socui

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// QuotedArgs returns a byte slice with each arg appended separated by a space.
// Any arg that contains a space is quoted with strconv.
func QuotedArgs(args []string) []byte {
	n := len(args)
	for _, arg := range args {
		n += 2 * len(arg)
	}
	b := make([]byte, 0, n)
	return appendQuotedArgs(b, args)
}

func appendQuotedArgs(b []byte, args []string) []byte {
	first := true
	for _, arg := range args {
		if first {
			first = false
		} else {
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
				return i + width, data[start : i+width], nil
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

func unquoteArg(arg string) string {
	if len(arg) < 2 || (arg[0] != '"' && arg[0] != '\'') {
		return arg
	}
	q := arg[0]
	arg = arg[1:]
	var buf strings.Builder
	buf.Grow(len(arg))
	for len(arg) > 0 && arg[0] != q {
		r, _, tail, err := strconv.UnquoteChar(arg, q)
		if err != nil {
			buf.WriteString(arg)
			break
		}
		buf.WriteRune(r)
		arg = tail
	}
	return buf.String()
}
