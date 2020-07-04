package socutil

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
	return AppendQuotedArgs(b, args)
}

// AppendQuotedArgs appends each elemenet of args, quoting them with strconv if
// they contain a space.
func AppendQuotedArgs(b []byte, args []string) []byte {
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

// ScanArgs implements a bufio.SplitFunc that will scan optionally quted arg tokens.
func ScanArgs(data []byte, atEOF bool) (advance int, token []byte, err error) {
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
