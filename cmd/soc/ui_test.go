package main

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jcorbin/soc/internal/socui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ui(t *testing.T) {
	expectAvail := expectLines(
		"## Available Commands",
		starLines(
			regexp.MustCompile(`^- \w+\s*:.*$`),
		),
	)

	runUITest(t,
		time.Date(2020, 7, 23, 1, 2, 3, 0, time.UTC),

		cmd(nil, expectLines(
			"# Usage",
			"> socTest [command args...]",
			"",
			expectAvail,
		)),

		cmd([]string{"help"}, expectLines(
			"# Usage",
			"> socTest help [command]",
			"",
			expectAvail,
		)),

		cmd([]string{"list"}, expectLines(
			"stream is empty, run `socTest today` to initialize",
			"... or just start adding items with `socTest <todo|wip|done> ...`",
		)),

		cmd([]string{"today"}, expectLines(
			"Created new Today section at top of stream",
			"",
			"# 2020-07-23",
			"1. TODO",
			"2. WIP",
			"3. Done",
		)),
		expectStream(expectLines(
			"# 2020-07-23",
			"",
			"## TODO",
			"",
			"## WIP",
			"",
			"## Done",
			"",
		)),

		// TODO use commands to build up to this, rather than faking
		fakeStream("for list",
			"# 2020-07-23\n",
			"## TODO\n",
			"- the other thing\n",
			"## WIP\n",
			"- that\n",
			"## Done\n",
			"- this\n",
			"# 2020-07-22\n",
			"- different things\n",
		),

		cmd([]string{"list"}, expectLines(
			"# 2020-07-23",
			"1. TODO",
			"2. WIP",
			"3. Done",
			"",
			"# 2020-07-22",
			"1. different things",
		)),

		// tomorrow
		24*time.Hour,
		cmd([]string{"today"}, expectLines(
			`Created Today by rolling "2020-07-23" forward`,
			"",
			"# 2020-07-24",
			"1. TODO",
			"   1. the other thing",
			"2. WIP",
			"   1. that",
			"3. Done",
		)),
		expectStream(expectLines(
			"# 2020-07-24",
			"",
			"## TODO",
			"- the other thing",
			"## WIP",
			"- that",
			"## Done",
			"# 2020-07-23",
			"- this",
			"# 2020-07-22",
			"- different things",
		)),

		cmd([]string{"todo"}, expectLines(
			"# 2020-07-24 TODO",
			"1. the other thing",
		)),

		// TODO use commands to build up to this, rather than faking
		fakeStream("for match",
			"# 2020-07-24\n",
			"\n",
			"## TODO\n",
			"- the other thing\n",
			"- and then...\n",
			"## WIP\n",
			"- that\n",
			"## Done\n",
			"# 2020-07-23\n",
			"- this\n",
			"# 2020-07-22\n",
			"- different things\n",
		),
		cmd([]string{"todo", "other"}, expectLines(
			"# 2020-07-24 TODO",
			"1. the other thing",
		)),
	)
}

func fakeStream(name string, parts ...string) uiTestStep {
	var content string
	for _, part := range parts {
		content += part
	}
	var ms memStore
	ms.set(content)
	if name == "" {
		name = "fake stream"
	} else {
		name = "fake stream: " + name
	}
	return named{name, true, uiTestSteps{
		withStorage{&ms},
		expectStream(content),
	}}
}

func runUITest(tt *testing.T, args ...interface{}) {
	var tc uiTestCompiler
	if step, err := tc.compile(args...); err != nil {
		require.NoError(tt, err)
	} else if step != nil {
		var t uiTestContext
		t.T = tt
		t.args = []string{"socTest"}
		step.run(&t)
	}
}

func (tc *uiTestCompiler) compile(args ...interface{}) (uiTestStep, error) {
	for _, arg := range args {
		switch val := arg.(type) {
		// sub-test stack ops
		case string: // open a named sub-test
			tc.push(val)
		case nil: // close a named sub-test
			tc.pop()

		// add a step to the stack head
		case time.Time: // set the test clock
			tc.add(then(val))
		case time.Duration: // advance the test clock
			tc.add(elapse(val))
		case store: // set storage (stream content)
			tc.add(withStorage{val})
		case uiTestArgs: // auto-name toplevel commands
			for tc.head().auto {
				tc.pop()
			}
			if tc.head().name == "" {
				tc.auto(fmt.Sprintf("cmd: %q", val.args))
			}
			tc.add(val)
		case streamExpecter: // auto-name stream expectations
			tc.auto("stream")
			tc.add(val)
		case uiTestStep: // any piece of test logic
			tc.add(val)

		default:
			return nil, fmt.Errorf("invalid ui test arg type %T", val)
		}
	}
	return tc.fin(), nil
}

type uiTestContext struct {
	*testing.T
	now time.Time
	ui
}

type uiTestStep interface {
	run(t *uiTestContext)
}

type withStorage struct{ store }

func (ws withStorage) run(t *uiTestContext) {
	t.store = ws.store
}

func expectStream(expectArgs ...interface{}) streamExpecter {
	return streamExpecter{expect(expectArgs...)}
}

type streamExpecter struct{ stringExpecter }

func (se streamExpecter) run(t *uiTestContext) {
	var buf bytes.Buffer
	if t.store == nil {
		buf.WriteString("<Store Is Nil>")
	} else {
		rc, err := t.store.open()
		require.NoError(t, err, "must open stream")
		_, err = io.Copy(&buf, rc)
		require.NoError(t, err, "must read stream")
		require.NoError(t, rc.Close(), "must read stream")
	}
	se.expect(t, buf.String())
}

type then time.Time
type elapse time.Duration

func (tm then) run(t *uiTestContext)  { t.now = time.Time(tm) }
func (d elapse) run(t *uiTestContext) { t.now = t.now.Add(time.Duration(d)) }

func cmd(args []string, expectArgs ...interface{}) (ta uiTestArgs) {
	ta.args = args
	for _, e := range expectArgs {
		switch v := e.(type) {
		case error:
			if ta.err != nil {
				panic("cmd already has an expected error ")
			}
			ta.err = v
		default:
			ta.output = expect(ta.output, v)
		}
	}
	return ta
}

type stringExpecter interface {
	expect(t testing.TB, s string)
}

type lineExpecter interface {
	expectLine(t testing.TB, n int, lines []string) []string
}

type stringExpecters []stringExpecter
type expectString string
type expectRegexp struct{ pattern *regexp.Regexp }
type anything struct{}

var expectAny = anything{}

type lineExpecters []lineExpecter

func expect(args ...interface{}) stringExpecter {
	var es stringExpecters
	var le stringExpecter
	for _, arg := range args {
		var ne stringExpecter
		switch v := arg.(type) {
		case nil:
			continue

		case string:
			if es, ok := le.(expectString); ok {
				le = expectString(string(es) + v)
				continue
			}
			ne = expectString(v)

		case *regexp.Regexp:
			ne = expectRegexp{v}

		case stringExpecter:
			ne = v

		default:
			panic(fmt.Sprintf("invalid expect arg type %T", arg))
		}
		if le != nil {
			es = append(es, le)
		}
		le = ne
	}
	if len(es) == 0 {
		return le
	}
	return append(es, le)
}

func expectLines(args ...interface{}) (ls lineExpecters) {
	for _, arg := range args {
		var e lineExpecter
		switch v := arg.(type) {
		case nil:
			continue

		case string:
			e = expectString(v)

		case *regexp.Regexp:
			e = expectRegexp{v}

		case lineExpecter:
			e = v

		case lineExpecters:
			ls = append(ls, v...)
			continue

		default:
			panic(fmt.Sprintf("invalid expectLines arg type %T", arg))
		}
		ls = append(ls, e)
	}
	return ls
}

func starLines(args ...interface{}) lineExpecter {
	switch ls := expectLines(args...); len(ls) {
	case 0:
		return nil
	case 1:
		return lineStar{ls[0]}
	default:
		return lineStar{ls}
	}
}

type lineStar struct{ lineExpecter }

func (star lineStar) expectLine(t testing.TB, n int, lines []string) []string {
	for len(lines) > 0 {
		var tmp testing.T
		r := star.lineExpecter.expectLine(&tmp, n, lines)
		if len(r) == len(lines) {
			break
		}
		rest := star.lineExpecter.expectLine(t, n, lines)
		if len(rest) == len(lines) {
			break
		}
		n += len(lines) - len(rest)
		lines = rest
	}
	return lines
}

func (es stringExpecters) expect(t testing.TB, s string) {
	for _, e := range es {
		e.expect(t, s)
	}
}

func (es expectString) expect(t testing.TB, s string) {
	assert.Equal(t, string(es), s, "expected output")
}

func (es expectString) expectLine(t testing.TB, n int, lines []string) []string {
	if assert.Equal(t, string(es), lines[0], "expected line #%v", n) {
		return lines[1:]
	}
	return lines
}

func (er expectRegexp) expect(t testing.TB, s string) {
	assert.Regexp(t, er.pattern, s, "expected output")
}

func (er expectRegexp) expectLine(t testing.TB, n int, lines []string) []string {
	if assert.Regexp(t, er.pattern, lines[0], "expected line #%v", n) {
		return lines[1:]
	}
	return lines
}

func (anything) expect(t testing.TB, s string)                           {}
func (anything) expectLine(t testing.TB, n int, lines []string) []string { return lines[1:] }

func (ls lineExpecters) expect(t testing.TB, s string) {
	lines := strings.Split(s, "\n")
	if i := len(lines) - 1; i > 0 && lines[i] == "" {
		lines = lines[:i]
	}
	n, rest := 1, lines
	if !runTB("lines", t, func(t testing.TB) {
		rest = ls.expectLine(t, n, rest)
	}) {
		m := n + len(lines) - len(rest)
		for i := 0; i <= len(lines); i++ {
			line := "<EOF>"
			if i < len(lines) {
				line = lines[i]
			}
			ln := n + i
			status := "?"
			if ln < m {
				status = "OK"
			} else if ln == m {
				status = "FAIL"
			}
			t.Logf("% 4v #%v: %v", status, ln, line)
		}
	}
}

func (ls lineExpecters) expectLine(t testing.TB, n int, lines []string) (rest []string) {
	rest = lines
	for i := 0; i < len(ls); i++ {
		if len(rest) == 0 {
			assert.Fail(t, "unexpected end of lines", "line #%v", n)
			break
		}
		rem := ls[i].expectLine(t, n, rest)
		if len(rem) != len(rest) {
			n += len(rest) - len(rem)
			rest = rem
		} else if t.Failed() {
			break
		}
	}
	return rest
}

type uiTestArgs struct {
	args   []string
	output stringExpecter
	err    error
}

func (ta uiTestArgs) run(t *uiTestContext) {
	var out bytes.Buffer
	err := socui.ArgsRequest(t.now, ta.args).Serve(&out, t)
	if ta.err != nil {
		assert.EqualError(t, err, ta.err.Error())
	} else if !assert.NoError(t, err, "unexpected error") {
		for i, line := range strings.Split(out.String(), "\n") {
			t.Logf("out #%v: %s", i+1, line)
		}
		t.FailNow()
	}
	if ta.output != nil {
		ta.output.expect(t, out.String())
	}
}

type named struct {
	name string
	auto bool
	uiTestStep
}

func (n named) run(t *uiTestContext) {
	t.Run(n.name, func(tt *testing.T) {
		defer func(tt *testing.T) { t.T = tt }(t.T)
		t.T = tt
		n.uiTestStep.run(t)
	})
}

type uiTestSteps []uiTestStep
type uiTestAllSteps uiTestSteps

func (steps uiTestSteps) run(t *uiTestContext) {
	for _, step := range steps {
		if t.Failed() {
			break
		}
		step.run(t)
	}
}

func (steps uiTestAllSteps) run(t *uiTestContext) {
	for _, step := range steps {
		step.run(t)
	}
	if t.Failed() {
		t.FailNow()
	}
}

func appendTestStep(a uiTestStep, bs ...uiTestStep) uiTestStep {
	steps, ok := a.(uiTestSteps)
	if !ok && a != nil {
		steps = uiTestSteps{a}
	}
	for _, b := range bs {
		if more, ok := b.(uiTestSteps); ok {
			steps = append(steps, more...)
		} else if b != nil {
			steps = append(steps, b)
		}
	}
	switch len(steps) {
	case 0:
		return nil
	case 1:
		return steps[0]
	default:
		return steps
	}
}

type uiTestCompiler struct {
	stack []named
}

func (tc *uiTestCompiler) head() named {
	if i := len(tc.stack) - 1; i >= 0 {
		return tc.stack[i]
	}
	return named{}
}

func (tc *uiTestCompiler) add(step uiTestStep) {
	if i := len(tc.stack) - 1; i >= 0 {
		tc.stack[i].uiTestStep = appendTestStep(tc.stack[i].uiTestStep, step)
	} else {
		tc.stack = append(tc.stack, named{uiTestStep: step})
	}
}

func (tc *uiTestCompiler) auto(name string) {
	tc.stack = append(tc.stack, named{name: name, auto: true})
}

func (tc *uiTestCompiler) push(name string) {
	tc.stack = append(tc.stack, named{name: name})
}

func (tc *uiTestCompiler) pop() bool {
	i := len(tc.stack) - 1
	if i < 1 {
		return false
	}
	head := tc.stack[i]
	tc.stack = tc.stack[:i]
	i--
	if steps, ok := head.uiTestStep.(uiTestSteps); ok {
		if _, isCmdSeq := steps[0].(uiTestArgs); isCmdSeq {
			head.uiTestStep = uiTestAllSteps(steps)
		}
	}
	tc.add(head)
	return true
}

func (tc *uiTestCompiler) fin() uiTestStep {
	for tc.pop() {
	}
	ztep := tc.stack[0]
	tc.stack = tc.stack[:0]
	if ztep.name != "" {
		return ztep
	}
	return ztep.uiTestStep
}

func runTB(name string, t testing.TB, under func(testing.TB)) bool {
	type testRunner interface {
		Run(string, func(*testing.T)) bool
	}
	if tr, ok := t.(testRunner); ok {
		return tr.Run(name, func(t *testing.T) { under(t) })
	} else if t.Failed() {
		return false
	} else {
		under(t)
		return !t.Failed()
	}
}
