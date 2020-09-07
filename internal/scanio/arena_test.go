package scanio_test

import (
	"fmt"
	"io"
	"io/ioutil"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jcorbin/soc/internal/scanio"
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

func Test_arena_ReadAt(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		empty := strings.NewReader("")
		var far FileArena
		far.Reset(empty, 0)
		testReader(t, far, empty,
			readOp{0, 0}, // ""
			readOp{1, 0}, // ""
		)
	})

	lorem := strings.NewReader(loremIpsum)
	loremOps := []readOp{
		{0, 0}, // ""
		{5, 0}, // "Lorem"
		{5, 6}, // "ipsum"
		{10, 100},
		{10, 200},
		{10, 400},
		{10, 800},
		{10, 1600},
	}
	t.Run("lorem ipsum", func(t *testing.T) {
		var far FileArena
		far.Reset(lorem, 0)
		testReader(t, far, lorem, loremOps...)
	})

	t.Run("lorem ipsum (smol buf)", func(t *testing.T) {
		var far FileArena
		far.Reset(lorem, 0)
		far.SetBufSize(16)
		testReader(t, far, lorem, loremOps...)
	})
}

func TestToken_ReadAt(t *testing.T) {
	var far FileArena
	far.Reset(strings.NewReader(loremIpsum), 0)
	far.SetBufSize(32)

	sentPat := regexp.MustCompile(`(?s)\s*(.+?\.)\s*`)
	sents := sentPat.FindAllStringSubmatchIndex(loremIpsum, -1)
	if !assert.True(t, len(sents) > 2, "must have at least two sentences") {
		return
	}

	tok := make([]scanio.Token, len(sents))
	txt := make([]string, len(sents))
	for i, match := range sents {
		t.Run(fmt.Sprintf("sentence[%v]", i), func(t *testing.T) {
			start, end := match[2], match[3]
			tok[i] = far.Ref(start, end)
			txt[i] = loremIpsum[start:end]
			t.Logf("start:%v end:%v content:%q", start, end, txt[i])
			testReader(t, tok[i], strings.NewReader(txt[i]), pruneOps(len(txt[i]),
				readOp{0, 5},
				readOp{5, 15},
				readOp{10, 50},
				readOp{10, 70},
				readOp{30, 70},
				readOp{10, 90},
				readOp{30, 90},
				readOp{60, 90},
			)...)
		})
	}

	t.Run("sub-Token in sentence[1]", func(t *testing.T) {
		if assert.True(t, len(txt[1]) >= 20, "short second sentence") {
			testReader(t,
				tok[1].Ref(5, 15),
				strings.NewReader(txt[1][5:15]),
				readOp{0, 5},
				readOp{0, 10},
				readOp{5, 10},
			)
		}
	})
}

func pruneOps(n int, ops ...readOp) []readOp {
	for i, op := range ops {
		if n < int(op.off)+op.n {
			return ops[:i]
		}
	}
	return ops
}

func testReader(t *testing.T, subject, target io.ReaderAt, ops ...readOp) {
	for _, op := range ops {
		t.Run(fmt.Sprintf("%v@%v", op.n, op.off), spiedTest(t, subject, target, func(t *testing.T, subject, target io.ReaderAt) {
			op.run(t, subject, target)
		}))
	}
	t.Run("full content", spiedTest(t, subject, target, func(t *testing.T, subject, target io.ReaderAt) {
		assert.Equal(t, readerContent(target), readerContent(subject))
	}))
}

func spiedTest(
	t *testing.T, subject, target io.ReaderAt,
	fn func(t *testing.T, subject, target io.ReaderAt),
) func(*testing.T) {
	return func(*testing.T) {
		var silent testing.T
		fn(&silent, subject, target)
		if silent.Failed() {
			subject = spyReaderAt(t, "subject", subject)
			target = spyReaderAt(t, "target", target)
			fn(&silent, subject, target)
		}
	}
}

func spyReaderAt(t *testing.T, name string, ra io.ReaderAt) io.ReaderAt {
	t.Logf("%s: %#v %#v", name, ra, ArenaOf(ra))
	return readerAtSpy{ra, func(sp []byte, off int64, sn int, se error) {
		t.Logf("%s %T.ReadAt(n:%v, off:%v) => (n:%v, err:%v, s:%q)", name, ra, len(sp), off, sn, se, sp[:sn])
	}}
}

type readerAtSpy struct {
	io.ReaderAt
	fn func(p []byte, off int64, n int, err error)
}

func (ras readerAtSpy) ReadAt(p []byte, off int64) (n int, err error) {
	n, err = ras.ReaderAt.ReadAt(p, off)
	ras.fn(p, off, n, err)
	return n, err
}

func (ras readerAtSpy) Size() int64 {
	return readerSize(ras.ReaderAt)
}

type readOp struct {
	n   int
	off int64
}

func (op readOp) run(t *testing.T, subject, target io.ReaderAt) {
	ap := make([]byte, op.n)
	bp := make([]byte, op.n)
	an, ae := target.ReadAt(ap, op.off)
	bn, be := subject.ReadAt(bp, op.off)
	assert.Equal(t, readResult(ap, an), readResult(bp, bn), "expected output to match")
	if ae == nil {
		assert.NoError(t, be, "unexpected error")
	} else {
		assert.EqualError(t, be, ae.Error(), "expected error")
	}
	if t.Failed() {
		t.Logf("expected n:%v err:%v", an, ae)
		t.Logf("got n:%v err:%v", bn, be)
	}
}

func readerContent(ra io.ReaderAt) string {
	sz := readerSize(ra)
	r := io.NewSectionReader(ra, 0, sz)
	return byteResult(ioutil.ReadAll(r))
}

func readerSize(ra io.ReaderAt) int64 {
	if szr, ok := ra.(interface{ Size() int64 }); ok {
		return szr.Size()
	}
	return 0
}

func readResult(p []byte, n int) string {
	return byteResult(bytesRead(p, n))
}

func byteResult(b []byte, err error) string {
	if err != nil {
		return fmt.Sprintf("!ERROR(%v)", err)
	}
	return string(b)
}

func bytesRead(p []byte, n int) (b []byte, err error) {
	defer func() {
		if e := recover(); e != nil {
			if er, ok := e.(error); ok {
				err = er
			} else {
				err = fmt.Errorf("paniced: %v", e)
			}
		}
	}()
	return p[:n], nil
}
