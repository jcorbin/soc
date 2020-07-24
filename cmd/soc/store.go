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
