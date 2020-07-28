package socutil

import "io"

// CopySection is essentially a fused version of io.Copy(dst, io.NewSectionReader(ra, off, n)).
// In other words it copies a byte range from the src reader into the dst writer stream.
// Allocates a temporary copyBuf if given nil.
// Returns the number of bytes written and any write or read error.
func CopySection(dst io.Writer, src io.ReaderAt, off, n int64, copyBuf []byte) (written int64, err error) {
	if copyBuf == nil {
		copyBuf = make([]byte, 32*1024)
	}
	for limit := off + n; off < limit; {
		p := copyBuf
		if max := int(limit - off); len(p) > max {
			p = p[:max]
		}
		nr, er := src.ReadAt(p, off)
		off += int64(nr)
		if p = p[:nr]; len(p) > 0 {
			nw, ew := dst.Write(p)
			written += int64(nw)
			if ew != nil {
				return written, ew
			}
			if nw != nr {
				return written, io.ErrShortWrite
			}
		}
		if er == io.EOF {
			return written, nil
		} else if er != nil {
			return written, er
		}
	}
	return written, nil
}
