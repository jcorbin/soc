package scanio

// Scanner abstracts over tokenizing scanners, like bufio.Scanner.
// Its Scan() method should return true if another token has been scanned from
// input, false otherwise (at EOF, read error, parse error, etc).
type Scanner interface {
	Scan() bool
	Bytes() []byte
}

// ErrScanner is a Scanner extension implemented by scanners that potentially
// need to return a scan error. This will typically be a read error from the
// input io.Reader, or a parse error from the token split function.
type ErrScanner interface {
	Scanner
	Err() error
}

// ScanError returns any scan error retained by the given Scanner.
// See the ErrScanner extension.
func ScanError(sc Scanner) (err error) {
	if esc, ok := sc.(ErrScanner); ok {
		err = esc.Err()
	}
	return err
}
