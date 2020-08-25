package scanio_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/jcorbin/soc/internal/scanio"
)

func TestEditor(t *testing.T) {
	v1 := runEditorTest(t, "write v1", func(t *testing.T, ed *testEditor) {
		a := ed.CursorAt(0)
		io.WriteString(a, "hello\n")
		ed.expect(t, "hello\n")
		assert.Equal(t, 6, a.Location())

		b := a.Copy()
		b.By(-1)
		assert.Equal(t, 5, b.Location())

		wn, _ := io.WriteString(a, "world\n")
		ed.expect(t, "hello\nworld\n")
		assert.Equal(t, 12, a.Location())
		assert.Equal(t, 5, b.Location())

		assert.Equal(t, 6, wn)
		c := a.Copy()
		assert.Equal(t, 12, c.Location())
		c.By(-wn)
		assert.Equal(t, 6, c.Location())

		io.WriteString(b, " there")
		ed.expect(t, "hello there\nworld\n")
		assert.Equal(t, 18, a.Location())
		assert.Equal(t, 11, b.Location())
		assert.Equal(t, 12, c.Location())

		io.WriteString(c, "now ")
		ed.expect(t, "hello there\nnow world\n")
		assert.Equal(t, 22, a.Location())
		assert.Equal(t, 11, b.Location())
		assert.Equal(t, 16, c.Location())

		io.WriteString(a, "...\n")
		ed.expect(t, "hello there\nnow world\n...\n")
		assert.Equal(t, 26, a.Location())
		assert.Equal(t, 11, b.Location())
		assert.Equal(t, 16, c.Location())
	})

	runEditorTest(t, "edit v2", func(t *testing.T, ed *testEditor) {
		var far FileArena
		require.NoError(t, far.Reset(v1, 0), "must load v1 FileArena")
		ed.Append(far.RefAll())
		// hello there
		// now world
		// ...

		ed.Remove(far.Ref(5, 15))
		ed.expect(t, "hello world\n...\n", "after remove")
		t.Logf("editor after remove: %+v", ed)

		cur := ed.CursorAt(6)
		t.Logf("cursor @6: %+v", cur)
		io.WriteString(cur, "brave new ")
		ed.expect(t, "hello brave new world\n...\n", "after insert")
		t.Logf("editor after insert: %+v", ed)

		require.NoError(t, cur.Close(), "must close cursor")
	})
}

type testEditor struct {
	Editor
	tmp bytes.Buffer
}

func runEditorTest(t *testing.T, name string, with func(t *testing.T, ed *testEditor)) io.ReaderAt {
	var ed testEditor
	t.Run(name, func(t *testing.T) {
		with(t, &ed)
	})
	return ed.save()
}

func (ed *testEditor) expect(t testing.TB, content string, msgAndArgs ...interface{}) {
	ed.tmp.Reset()
	ed.WriteTo(&ed.tmp)
	assert.Equal(t, content, ed.tmp.String(), msgAndArgs...)
}

func (ed *testEditor) save() io.ReaderAt {
	ed.tmp.Reset()
	ed.WriteTo(&ed.tmp)
	return strings.NewReader(ed.tmp.String())
}
