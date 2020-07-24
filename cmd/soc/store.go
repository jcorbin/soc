package main

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
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
