package scanio

import "io"

// CopyScanner scans all tokens from the src scanner, writing their bytes into
// the dst writer.
// Stops on first non-nil write error, returning the number of bytes written
// into dst and any error.
func CopyScanner(dst io.Writer, src Scanner) (n int64, err error) {
	for err == nil && src.Scan() {
		var m int
		m, err = dst.Write(src.Bytes())
		n += int64(m)
	}
	return n, err
}

// CopyScannerWith scans all tokens from the src scanner, writing their bytes
// into the dst writer with sep bytes between every token.
// Does not write a final ending separator.
// Stops on first non-nil write error, returning the number of bytes written
// into dst and any error.
func CopyScannerWith(dst io.Writer, src Scanner, sep []byte) (n int64, err error) {
	first := true
	for err == nil && src.Scan() {
		var m int
		if first {
			first = false
		} else {
			m, err = dst.Write(sep)
			n += int64(m)
		}
		m, err = dst.Write(src.Bytes())
		n += int64(m)
	}
	return n, err
}
