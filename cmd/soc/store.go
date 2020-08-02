package main

import (
	"bytes"
	"errors"
	"github.com/jcorbin/soc/internal/socutil"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

var (
	errStoreExists    = errors.New("stream already exists")
	errStoreNotExists = errors.New("stream does not exist")
	errBufferClosed   = errors.New("write to closed buffer")
)

type store interface {
	open() (io.ReadCloser, error)
	create() (cleanupWriteCloser, error)
	update() (cleanupWriteCloser, error)
}

type ReadAtCloser interface {
	io.ReaderAt
	io.Closer
}

// sizedReaderAt converts the given read stream into a reader at, and returns it size.
// To achieve this, it may sponge() the stream into an anonymous temp file.
//
// The caller is responsible for closing any returned ReadAtCloser.
// When no error is returned, the caller is no longer responsible for closing rc:
// it has either been closed already, or cast into the returned ReadAtCloser.
func sizedReaderAt(rc io.ReadCloser) (ReadAtCloser, int64, error) {
	rac, ok := rc.(ReadAtCloser)
	var size int64
	if ok {
		size, ok = readerSize(rc)
	}
	if !ok {
		// sponge into an orphaned tmp file if necessary
		f, err := sponge(rc)
		if err != nil {
			return nil, 0, err
		}
		rac = f
		if st, err := f.Stat(); err != nil {
			return nil, 0, err
		} else {
			size = st.Size()
		}
		if cerr := rc.Close(); err == nil {
			err = cerr
		}
	}
	return rac, size, nil
}

func readerSize(r io.Reader) (int64, bool) {
	if able, ok := r.(interface{ Stat() (os.FileInfo, error) }); ok {
		if st, err := able.Stat(); err == nil {
			return st.Size(), true
		}
	}
	return 0, false
}

// sponge copies all data from the given reader into a new temporary file to
// support future random access (using ReadAt and/or Seek+Read).
//
// The returned temp file is "anonymous": it has already been removed from the
// filesystem, and so its data only exists as long as it remains open.
func sponge(r io.Reader) (_ *os.File, rerr error) {
	tmp, err := ioutil.TempFile("", "")
	if err != nil {
		return nil, err
	}
	defer func() {
		if rerr != nil {
			os.Remove(tmp.Name())
			tmp.Close()
		}
	}()
	if _, err := io.Copy(tmp, r); err != nil {
		return nil, err
	}
	os.Remove(tmp.Name())
	return tmp, nil
}

type cleanupWriteCloser interface {
	io.WriteCloser
	Cleanup() error
}

type memStore struct {
	cur     string
	defined bool
}

func (ms *memStore) open() (io.ReadCloser, error) {
	if !ms.defined {
		return nil, errStoreNotExists
	}
	return ioutil.NopCloser(strings.NewReader(ms.cur)), nil
}

func (ms *memStore) create() (cleanupWriteCloser, error) {
	if ms.defined {
		return nil, errStoreExists
	}
	return ms.pendBuf(ms.set), nil
}

func (ms *memStore) update() (cleanupWriteCloser, error) {
	return ms.pendBuf(ms.set), nil
}

func (ms *memStore) pendBuf(sink func(string) error) *pendingBuffer {
	const minSize = 1024
	pb := &pendingBuffer{sink: ms.set}
	if n := len(ms.cur); n > minSize {
		pb.buf.Grow(n)
	} else {
		pb.buf.Grow(minSize)
	}
	return pb
}

func (ms *memStore) set(content string) error {
	ms.cur = content
	ms.defined = true
	return nil
}

type pendingBuffer struct {
	buf    bytes.Buffer
	closed bool
	sink   func(string) error
}

func (pms *pendingBuffer) Write(p []byte) (int, error) {
	if pms.closed {
		return 0, errBufferClosed
	}
	return pms.buf.Write(p)
}

func (pms *pendingBuffer) WriteString(s string) (int, error) {
	if pms.closed {
		return 0, errBufferClosed
	}
	return pms.buf.WriteString(s)
}

func (pms *pendingBuffer) Close() error {
	if !pms.closed {
		pms.closed = true
		return pms.sink(pms.buf.String())
	}
	return nil
}

func (pms *pendingBuffer) Cleanup() error {
	if !pms.closed {
		// discarded
		pms.closed = true
	}
	return nil
}

type fsStore struct {
	filename string
	fileinfo os.FileInfo
}

func (fst *fsStore) open() (io.ReadCloser, error) {
	if fst.fileinfo == nil {
		info, err := os.Stat(fst.filename)
		if err != nil {
			return nil, err
		}
		fst.fileinfo = info
	}
	if fst.fileinfo == nil {
		return nil, errStoreNotExists
	}
	return os.Open(fst.filename)
}

func (fst *fsStore) create() (cleanupWriteCloser, error) {
	if fst.fileinfo != nil {
		return nil, errStoreExists
	}
	f, err := os.OpenFile(fst.filename, os.O_EXCL|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return nil, err
	}
	return &pendingCreateFile{File: f}, nil
}

func (fst *fsStore) update() (cleanupWriteCloser, error) {
	if fst.fileinfo == nil {
		return nil, errStoreNotExists
	}
	f, err := ioutil.TempFile(filepath.Dir(fst.filename), "."+filepath.Base(fst.filename)+".tmp_*")
	if err != nil {
		return nil, err
	}
	return &pendingUpdateFile{File: f, dest: fst.filename}, nil
}

type pendingUpdateFile struct {
	*os.File
	dest   string
	closed bool
}

func (uf *pendingUpdateFile) Close() error {
	err := uf.Sync()

	if cerr := uf.File.Close(); err == nil {
		err = cerr
	}

	if err == nil {
		err = os.Rename(uf.Name(), uf.dest)
	}

	uf.closed = err == nil
	return err
}

func (uf *pendingUpdateFile) Cleanup() error {
	if uf.closed {
		return nil
	}
	err := os.Remove(uf.Name())
	if cerr := uf.Close(); err == nil {
		err = cerr
	}
	uf.closed = true
	return err
}

type pendingCreateFile struct {
	*os.File
	closed bool
}

func (cf *pendingCreateFile) Close() error {
	if cf.closed {
		return nil
	}
	err := cf.File.Close()
	cf.closed = err == nil
	return err
}

func (cf *pendingCreateFile) Cleanup() error {
	if cf.closed {
		return nil
	}
	err := os.Remove(cf.Name())
	if cerr := cf.File.Close(); err == nil {
		err = cerr
	}
	cf.closed = true
	return err
}

type byteRange struct {
	start int64
	end   int64
}

type byteRanges []byteRange

// TODO func (brs *byteRanges) add(br byteRange) -- is more complicated, try to use sub instead
func (brs *byteRanges) sub(br byteRange) {
	tmp := (*brs)
	i, j := 0, len(tmp) // indices for later merge

	// split: heads and tails into two contiguous sorted regions
	for i, prior := range *brs {
		head, tail := prior.sub(br)
		tmp[i] = head
		if !tail.empty() {
			tmp = append(tmp, tail)
		}
	}

	// merge: but with a stronger property than usual since, by original
	// disjointness, we can simply swap a head value into the tail cursor,
	// without any need to do a tail insortion
	res := tmp[:0]
	for k := j; ; {
		// read head cursor, eliding empty ranges
		var headVal byteRange
		haveI := i < k
		if haveI {
			if headVal = tmp[i]; headVal.empty() {
				i++
				continue
			}
		}

		// read tail cursor, eliding empty ranges
		var tailVal byteRange
		haveJ := j < len(tmp)
		if haveJ {
			if tailVal = tmp[j]; tailVal.empty() {
				j++
				continue
			}
		}

		// done once both cursors run out
		if !haveI && !haveJ {
			break
		}

		// finalize tail cursor
		if !haveI {
			res = append(res, tailVal)
			j++
			continue
		}

		// finalize head cursor
		if !haveJ {
			res = append(res, headVal)
			i++
			continue
		}

		// we have two active cursors, compare them and advance the head side,
		// maybe stashing a head value in the tail
		if tailVal.start < headVal.start {
			// NOTE this is valid due to the disjointness property discussed above
			tmp[j] = headVal
			headVal = tailVal
		}
		res = append(res, headVal)
		i++
	}

	*brs = res
}

func (br byteRange) empty() bool {
	return br.end <= br.start
}

func (br byteRange) headPoint() byteRange {
	br.end = br.start
	return br
}

func (br byteRange) tailPoint() byteRange {
	br.start = br.end
	return br
}

func (br byteRange) intersect(other byteRange) byteRange {
	if br.start < other.start && other.start < br.end {
		br.start = other.start
	}
	if br.end > other.end && other.end > br.start {
		br.end = other.end
	}
	return br
}

func (br byteRange) sub(other byteRange) (head, tail byteRange) {
	head = br
	if other.start < br.end {
		if other.end < br.start {
			return
		}
		head.end = other.start
		if head.end < head.start {
			head = byteRange{}
		}

		if other.end < br.end {
			tail = br
			tail.start = other.end
		}
	}
	return
}

// TODO eventually unify readState/byteRange into a file-backed scanio arena
type readState struct {
	ReadAtCloser
	size int64
}

func (rs *readState) Close() error {
	if rs.ReadAtCloser == nil {
		return nil
	}
	err := rs.ReadAtCloser.Close()
	if err == nil {
		rs.ReadAtCloser = nil
		rs.size = 0
	}
	return err
}

func (rs *readState) Size() int64 {
	return rs.size
}
func (rs *readState) open(rc io.ReadCloser, err error) error {
	if errors.Is(err, errStoreNotExists) {
		return nil
	}
	if err != nil {
		return err
	}
	rs.ReadAtCloser, rs.size, err = sizedReaderAt(rc)
	return err
}
type writeState struct {
	w   io.Writer
	err error
}

func (ws *writeState) Write(p []byte) (n int, err error) {
	if err = ws.err; err == nil {
		n, err = ws.w.Write(p)
		ws.err = err
	}
	return n, err
}

func (ws *writeState) WriteString(s string) (n int, err error) {
	if err = ws.err; err == nil {
		n, err = io.WriteString(ws.w, s)
		ws.err = err
	}
	return n, err
}

type copyState struct {
	readState
	writeState
	copyBuf []byte
}

func (cs *copyState) init() {
	if cs.copyBuf == nil {
		cs.copyBuf = make([]byte, 32*1024)
	}
}

func (cs *copyState) copySection(br byteRange) error {
	if cs.err == nil {
		cs.init()
		// TODO CopySection just turns around and recomputes end...
		_, cs.err = socutil.CopySection(cs.w, cs.ReadAtCloser, br.start, br.end-br.start, cs.copyBuf)
	}
	return cs.err
}

func (cs *copyState) copySections(brs ...byteRange) error {
	if len(brs) > 0 && cs.err == nil {
		cs.init()
		for _, br := range brs {
			// TODO CopySection just turns around and recomputes end...
			_, cs.err = socutil.CopySection(cs.w, cs.ReadAtCloser, br.start, br.end-br.start, cs.copyBuf)
		}
	}
	return cs.err
}
