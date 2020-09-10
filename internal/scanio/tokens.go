package scanio

import "bytes"

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

