package scanio

import "io"

type TestArena struct{ arena }
func (ta *TestArena) SetBacking(back io.ReaderAt) { ta.setBack(back) }
func (ta *TestArena) SetBackErr(backErr error)    { ta.backErr = backErr }
func (ta *TestArena) Ref(start, end int) (tok Token) {
	tok.arena = &ta.arena
	tok.start, tok.end = start, end
	return tok
}

func ArenaOf(x interface{}) interface{} {
	switch val := x.(type) {
	case FileArena:
		return val.arena
	case *FileArena:
		return val.arena
	case ByteArena:
		return &val.arena
	case *ByteArena:
		return &val.arena
	case Token:
		return val.arena
	case Area:
		return val.arena
	default:
		return nil
	}
}
