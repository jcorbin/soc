package main

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_memStore(t *testing.T) {
	storeTest{store: &memStore{}}.run(t)
}

func Test_fsStore(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "fsStore")
	require.NoError(t, err, "must create temp dir")
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	filename := filepath.Join(tmpDir, "stream.md")
	storeTest{
		store: &fsStore{filename: filename},
		post: func(t *testing.T, content string) {
			b, err := ioutil.ReadFile(filename)
			if assert.NoError(t, err, "unexpected read error") {
				assert.Equal(t, content, string(b), "expected file content")
			}
		},
	}.run(t)
}

type storeTest struct {
	store
	post func(t *testing.T, content string)
}

func (st storeTest) run(t *testing.T) {
	for _, step := range []struct {
		name string
		fn   func(t *testing.T)
	}{
		{"initial open fail", st.noInitOpen},
		{"init create (not)", st.writeWith(st.create, "")},
		{"initial open fail (still)", st.noInitOpen},
		{"init create (actual)", st.writeWith(st.create, "actual")},
		{"create should now fail", st.createFails},
		{"read back", st.expect("actual")},
		{"update", st.writeWith(st.update, "actually")},
		{"read back 2", st.expect("actually")},
	} {
		if !t.Run(step.name, step.fn) {
			break
		}
	}

}

func (st storeTest) noInitOpen(t *testing.T) {
	_, err := st.open()
	assert.Error(t, err, "open should fail")
}

func (st storeTest) writeWith(open func() (cleanupWriteCloser, error), content string) func(t *testing.T) {
	return func(t *testing.T) {
		w, err := open()
		require.NoError(t, err, "must open for writing")
		defer func() {
			assert.NoError(t, w.Cleanup(), "cleanup should succeed")
		}()
		if content != "" {
			if _, err := io.WriteString(w, content); assert.NoError(t, err, "must write") {
				assert.NoError(t, w.Close(), "must close")
			}
		}
	}
}

func (st storeTest) createFails(t *testing.T) {
	_, err := st.create()
	assert.Error(t, err, "create should fail")
}

func (st storeTest) expect(content string) func(t *testing.T) {
	return func(t *testing.T) {
		r, err := st.open()
		require.NoError(t, err, "must open")
		if b, err := ioutil.ReadAll(r); assert.NoError(t, err, "must read") {
			if assert.NoError(t, r.Close(), "must read and close") {
				assert.Equal(t, content, string(b), "expected content")
				if st.post != nil {
					t.Run("post", func(t *testing.T) { st.post(t, content) })
				}
			}
		}
	}
}
