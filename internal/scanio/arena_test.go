package scanio_test

import (
	"fmt"
	"io"
	"strings"
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

func Test_arena_back(t *testing.T) {
	type check struct {
		start, end int
		expect     string
		err        string
	}
	checkText := func(start, end int, s string) (c check) {
		c.start, c.end = start, end
		c.expect = s
		return c
	}
	checkErr := func(start, end int, s string) (c check) {
		c.start, c.end = start, end
		c.err = s
		return c
	}
	for _, tc := range []struct {
		scenario
		checks []check
	}{

		{
			scenario: smolLorem,
			checks: []check{
				checkText(0, 5, "Lorem"),
				checkErr(0, 33, "token size exceeds arena buffer capacity"),
				checkText(10, 40, "m dolor sit amet, consectetur "),
				checkErr(10, 50, "token size exceeds arena buffer capacity"),
				checkText(74, 80, "luctus"),
				checkText(151, 155, "non,"),
				checkText(884, 902, "felis et posuere.\n"),
				checkErr(884, 903, io.EOF.Error()),
			},
		},

		{
			scenario: defLorem,
			checks: []check{
				checkText(0, 74, "Lorem ipsum dolor sit amet, consectetur adipiscing elit. Maecenas aliquam\n"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ar := tc.setup()
			if cl, ok := ar.(io.Closer); ok {
				defer func() {
					assert.NoError(t, cl.Close(), "unexpected arena close error")
				}()
			}
			for i, c := range tc.checks {
				t.Run(fmt.Sprintf("[%v] @%v:%v", i, c.start, c.end), func(t *testing.T) {
					tok := ar.Ref(c.start, c.end)
					if s, err := tok.Text(); c.err != "" {
						if !assert.EqualError(t, err, c.err, "expected error") {
							t.Logf("text: %q", s)
						}
					} else if assert.NoError(t, err, "unexpected error") {
						assert.Equal(t, c.expect, s)
					}
				})
			}
		})
	}
}

type scenario struct {
	name    string
	back    io.ReaderAt
	backErr error
	bufSize int
}

type refArena interface {
	Ref(start, end int) Token
}

func (sc scenario) setup() refArena {
	if sc.bufSize != 0 || sc.backErr != nil {
		var ta TestArena
		ta.SetBacking(sc.back)
		ta.SetBackErr(sc.backErr)
		ta.SetBufSize(sc.bufSize)
		return &ta
	}

	var fa FileArena
	fa.Reset(sc.back, 0)
	return &fa
}

var smolLorem = scenario{
	name:    "lorem w/ 32 byte buffer",
	back:    strings.NewReader(loremIpsum),
	bufSize: 32,
}

var defLorem = scenario{
	name: "lorem w/ default size buffer",
	back: strings.NewReader(loremIpsum),
}

const loremIpsum = `Lorem ipsum dolor sit amet, consectetur adipiscing elit. Maecenas aliquam
luctus enim, vel porta orci egestas eu. Fusce metus neque, elementum ut enim
non, commodo blandit eros. Nunc aliquam, magna consequat feugiat venenatis,
lectus mauris aliquam ipsum, quis dictum lorem nisi sed lorem. Curabitur
gravida iaculis velit ut posuere. Vestibulum at vehicula mi. Curabitur ut magna
enim. Vestibulum scelerisque luctus neque vitae euismod. Proin imperdiet purus
et mauris consectetur, eget malesuada velit commodo. Cras eleifend egestas ante
vitae finibus. Cras tempus ipsum sed nunc auctor rutrum. Aenean rhoncus lorem
non pellentesque vehicula. Nunc in arcu blandit, tristique ex vel, tincidunt
mauris. Donec a ornare ipsum. Phasellus placerat tincidunt augue quis tempus.
Class aptent taciti sociosqu ad litora torquent per conubia nostra, per
inceptos himenaeos. Cras scelerisque id felis et posuere.
`

func TestArea_Add(t *testing.T) {
	var far FileArena
	far.Reset(strings.NewReader(loremIpsum), 0)
	var ar Area
	for _, tc := range []struct {
		name       string
		add        [2]int
		expectRepr string
		expectOut  string
	}{
		{
			name:       "empty on zero",
			add:        [2]int{0, 0},
			expectRepr: "[]",
		},
		{
			name:       "ipsum",
			add:        [2]int{6, 11},
			expectRepr: "[@6:11]",
			expectOut:  "ipsum",
		},
		{
			name:       "sit",
			add:        [2]int{17, 21},
			expectRepr: "[@6:11 @17:21]",
			expectOut:  "ipsum sit",
		},
		{
			name:       "dolor",
			add:        [2]int{11, 17},
			expectRepr: "[@6:21]",
			expectOut:  "ipsum dolor sit",
		},
		{
			name:       "sit amet",
			add:        [2]int{18, 26},
			expectRepr: "[@6:26]",
			expectOut:  "ipsum dolor sit amet",
		},
		{
			name:       "Lorem ipsum",
			add:        [2]int{0, 11},
			expectRepr: "[@0:26]",
			expectOut:  "Lorem ipsum dolor sit amet",
		},
		{
			name:       "elit",
			add:        [2]int{50, 55},
			expectRepr: "[@0:26 @50:55]",
			expectOut:  "Lorem ipsum dolor sit amet elit",
		},
		{
			name:       "adip",
			add:        [2]int{39, 44},
			expectRepr: "[@0:26 @39:44 @50:55]",
			expectOut:  "Lorem ipsum dolor sit amet adip elit",
		},
		{
			name:       "... elit.",
			add:        [2]int{22, 56},
			expectRepr: "[@0:56]",
			expectOut:  "Lorem ipsum dolor sit amet, consectetur adipiscing elit.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tok := far.Ref(tc.add[0], tc.add[1])
			ar.Add(tok)
			if tc.expectRepr != "" {
				repr := fmt.Sprintf("%+v", ar)
				if i := strings.IndexByte(repr, '['); i >= 0 {
					repr = repr[i:]
				}
				assert.Equal(t, tc.expectRepr, repr, "expected area representation")
			}
			if tc.expectOut != "" {
				assert.Equal(t, tc.expectOut, fmt.Sprintf("%v", ar), "expected area contents")
			}
		})
	}
}

func TestArea_Sub(t *testing.T) {
	var far FileArena
	far.Reset(strings.NewReader(loremIpsum), 0)
	var ar Area
	ar.Add(far.Ref(0, 26))
	for _, tc := range []struct {
		name       string
		add        [2]int
		expectRepr string
		expectOut  string
	}{
		{
			name:       "empty",
			add:        [2]int{0, 0},
			expectRepr: "[@0:26]",
			expectOut:  "Lorem ipsum dolor sit amet",
		},
		{
			name:       "ip",
			add:        [2]int{6, 8},
			expectRepr: "[@0:6 @8:26]",
			expectOut:  "Lorem sum dolor sit amet",
		},
		{
			name:       "do",
			add:        [2]int{12, 14},
			expectRepr: "[@0:6 @8:12 @14:26]",
			expectOut:  "Lorem sum lor sit amet",
		},
		{
			name:       "ipsum dolor sit_",
			add:        [2]int{6, 22},
			expectRepr: "[@0:6 @22:26]",
			expectOut:  "Lorem amet",
		},
		{
			name:       "Lorem_",
			add:        [2]int{0, 6},
			expectRepr: "[@22:26]",
			expectOut:  "amet",
		},
		{
			name:       "amet",
			add:        [2]int{22, 26},
			expectRepr: "[]",
			expectOut:  "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tok := far.Ref(tc.add[0], tc.add[1])
			ar.Sub(tok)
			if tc.expectRepr != "" {
				repr := fmt.Sprintf("%+v", ar)
				if i := strings.IndexByte(repr, '['); i >= 0 {
					repr = repr[i:]
				}
				assert.Equal(t, tc.expectRepr, repr, "expected area representation")
			}
			if tc.expectOut != "" {
				assert.Equal(t, tc.expectOut, fmt.Sprintf("%v", ar), "expected area contents")
			}
		})
	}
}
