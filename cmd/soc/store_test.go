package main

import (
	"io"
	"io/ioutil"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)
func Test_memStore(t *testing.T) {
	storeTest{&memStore{}}.run(t)
}

type storeTest struct{ store }

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
			_, err = io.WriteString(w, content)
			if err == nil {
				err = w.Close()
			}
			assert.NoError(t, err, "must write and close")
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
		b, err := ioutil.ReadAll(r)
		if err == nil {
			err = r.Close()
		}
		if assert.NoError(t, err, "must read and close") {
			assert.Equal(t, content, string(b), "expected content")
		}
	}
}
