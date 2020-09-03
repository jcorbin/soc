package main

import (
	"bytes"
	"errors"
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

// sizedReaderAt converts the given read stream into a reader at, and returns it size.
// To achieve this, it may sponge() the stream into an anonymous temp file.
//
// It closes r if it implements io.Closer and was sponged so that the caller
// need only concern itself with the returned io.ReaderAt after return.
func sizedReaderAt(r io.Reader) (ra io.ReaderAt, size int64, rerr error) {
	if ra, ok := r.(io.ReaderAt); ok {
		if size, ok := readerSize(r); ok {
			return ra, size, nil
		}
	}

	// sponge into an orphaned tmp file if necessary
	f, err := sponge(r)
	defer func() {
		if rerr != nil {
			f.Close()
		}
	}()
	if cl, ok := r.(io.Closer); ok {
		if cerr := cl.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		return nil, 0, err
	}

	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}

	return f, info.Size(), nil
}

func readerSize(r io.Reader) (int64, bool) {
	type sizer interface{ Size() int64 }
	type stater interface{ Stat() (os.FileInfo, error) }
	if able, ok := r.(sizer); ok {
		return able.Size(), true
	}
	if able, ok := r.(stater); ok {
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
