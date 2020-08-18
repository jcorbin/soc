package scanio_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	. "github.com/jcorbin/soc/internal/scanio"
)

func TestToken_Format(t *testing.T) {
	var (
		zero  zeroTokenGen
		empty = stringToken("")
		foo   = stringToken("foo")
		fbb   = stringToken("foo bar baz")
	)

	for _, tc := range []struct {
		tokenGen
		fmt string
		out string
	}{
		{zero, "%s", "!(ERROR token has no arena)"},
		{zero, "%q", "!(ERROR token has no arena)"},
		{zero, "%v", "!(ERROR token has no arena)"},

		{empty, "%s", ""},
		{empty, "%q", `""`},
		{empty, "%v", ""},

		{foo, "%s", "foo"},
		{foo, "%q", `"foo"`},
		{foo, "%v", "foo"},

		{fbb, "%.6s", "foo ba"},
		{fbb, "%.6q", `"foo ba"`},
		{fbb, "%.6v", "foo ba"},
	} {
		t.Run(fmt.Sprintf("in:%v fmt:%q", tc.String(), tc.fmt), func(t *testing.T) {
			assert.Equal(t, tc.out, fmt.Sprintf(tc.fmt, tc.gen()))
		})
	}
}

type tokenGen interface {
	String() string
	gen() Token
}

func stringToken(s string) stringTokenGen {
	return stringTokenGen(s)
}

type stringTokenGen string

func (sg stringTokenGen) String() string {
	return string(sg)
}

type zeroTokenGen struct{}

func (zeroTokenGen) gen() Token     { return Token{} }
func (zeroTokenGen) String() string { return "ø" }

func (sg stringTokenGen) gen() Token {
	var bar ByteArena
	bar.WriteString(string(sg))
	return bar.Take()
}
