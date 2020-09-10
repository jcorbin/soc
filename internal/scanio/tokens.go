package scanio

import (
	"bytes"
)

// Index is bytes.Index over the token's content bytes.
func (token Token) Index(sep []byte) int {
	return token.indexFunc(len(sep), func(s []byte) int {
		return bytes.Index(s, sep)
	})
}

// Index is bytes.IndexByte over the token's content bytes.
func (token Token) IndexByte(c byte) int {
	return token.indexFunc(0, func(s []byte) int {
		return bytes.IndexByte(s, c)
	})
}

func (token Token) indexFunc(minback int, fn func([]byte) int) int {
	var off int
	br := token.byteRange
	for {
		s, err := token.arena.bytes(br)
		if err != nil {
			return -1
		}
		i := fn(s)
		if i < 0 && len(s) < token.len() {
			if n := len(s) - minback; n > 0 {
				if br.start += n; br.len() > 0 {
					off += n
					continue
				}
			}
		}
		if i >= 0 {
			i += off
		}
		return i
	}
}

// Trim works like bytes.Trim, returning a sub-token slice over the token's
// content bytes instead.
func (token Token) Trim(s string) Token {
	var fn func([]byte) int
	switch len(s) {
	case 0:
		return token
	case 1:
		fn = byteTrimFunc(s[0])
	default:
		fn = cutsetTrimFunc(s)
	}
	var b [1]byte
	token = token.trimLeftFunc(b[:], fn)
	token = token.trimRightFunc(b[:], fn)
	return token
}

// TrimLeft works like bytes.Trim, returning a sub-token slice over the token's
// content bytes instead.
func (token Token) TrimLeft(s string) Token {
	var fn func([]byte) int
	switch len(s) {
	case 0:
	case 1:
		fn = byteTrimFunc(s[0])
	default:
		fn = cutsetTrimFunc(s)
	}
	var b [1]byte
	return token.trimLeftFunc(b[:], fn)
}

// TrimRight works like bytes.Trim, returning a sub-token slice over the
// token's content bytes instead.
func (token Token) TrimRight(s string) Token {
	var fn func([]byte) int
	switch len(s) {
	case 0:
		return token
	case 1:
		fn = byteTrimFunc(s[0])
	default:
		fn = cutsetTrimFunc(s)
	}
	var b [1]byte
	token = token.trimLeftFunc(b[:], fn)
	return token
}

func (token Token) trimLeftFunc(b []byte, fn func([]byte) int) Token {
	for token.len() > 0 {
		n, err := token.arena.ReadAt(b, int64(token.start))
		if n == 0 {
			break
		}
		m := fn(b[:n])
		if m == 0 {
			break
		}
		token.start += m
		if err != nil {
			break
		}
	}
	return token
}

func (token Token) trimRightFunc(b []byte, fn func([]byte) int) Token {
	for token.len() > 0 {
		n, err := token.arena.ReadAt(b, int64(token.end)-1)
		if n == 0 {
			break
		}
		m := fn(b[:n])
		if m == 0 {
			break
		}
		token.end -= m
		if err != nil {
			break
		}
	}
	return token
}

func byteTrimFunc(c byte) func(b []byte) int {
	return func(b []byte) (n int) {
		for i := 0; i < len(b); i++ {
			if b[i] != c {
				break
			}
			n++
		}
		return n
	}
}

func cutsetTrimFunc(cutset string) func(b []byte) int {
	return func(b []byte) (n int) {
		for i := 0; i < len(b); i++ {
			if !isInCutset(b[0], cutset) {
				break
			}
			n++
		}
		return n
	}
}

func isInCutset(b byte, cutset string) bool {
	for i := 0; i < len(cutset); i++ {
		if b == cutset[i] {
			return true
		}
	}
	return false
}
