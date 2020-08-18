package scanio

import "io"

type TestArena struct{ arena }
func (ta *TestArena) SetBacking(back io.ReaderAt) { ta.setBack(back) }
func (ta *TestArena) SetBackErr(backErr error)    { ta.backErr = backErr }
func (ta *TestArena) SetBufSize(bufSize int)      { ta.setBufSize(bufSize) }
func (ta *TestArena) Ref(start, end int) (tok Token) {
	tok.arena = &ta.arena
	tok.start, tok.end = start, end
	return tok
}
