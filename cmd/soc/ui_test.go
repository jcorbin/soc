package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/jcorbin/soc/internal/socui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ui(t *testing.T) {
	runUITest(t,
		time.Date(2020, 7, 23, 1, 2, 3, 0, time.UTC),

		cmd(nil,
			"# Usage\n",
			"> socTest [command args...]\n",
			"\n",
			"## Available Commands\n",
			"- help: show help overview or on a specific topic or command\n",
		),

		cmd([]string{"help"},
			"# Usage\n",
			"> socTest help [command]\n",
			"\n",
			"## Available Commands\n",
			"- help: show help overview or on a specific topic or command\n",
		),
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
	return named{name, uiTestSteps{
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
			if tc.head().name == "" {
				tc.add(named{fmt.Sprintf("cmd: %q", val.args), val})
			} else {
				tc.add(val)
			}
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

type expectStream string

func (expect expectStream) run(t *uiTestContext) {
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
	assert.Equal(t, string(expect), buf.String(), "expected stream content")
}

type then time.Time
type elapse time.Duration

func (tm then) run(t *uiTestContext)  { t.now = time.Time(tm) }
func (d elapse) run(t *uiTestContext) { t.now = t.now.Add(time.Duration(d)) }

func cmd(args []string, expect ...interface{}) (ta uiTestArgs) {
	ta.args = args
	for _, e := range expect {
		switch v := e.(type) {
		case string:
			ta.output += v
		case error:
			if ta.err != nil {
				panic("cmd already has an expected error ")
			}
			ta.err = v
		}
	}
	return ta
}

type uiTestArgs struct {
	args   []string
	output string
	err    error
}

func (ta uiTestArgs) run(t *uiTestContext) {
	var out bytes.Buffer
	err := socui.ArgsRequest(t.now, ta.args).Serve(&out, t)
	if ta.err != nil {
		assert.EqualError(t, err, ta.err.Error())
	} else {
		require.NoError(t, err, "unexpected error")
	}
	assert.Equal(t, ta.output, out.String(), "expected output")
}

type named struct {
	name string
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

func (steps uiTestSteps) run(t *uiTestContext) {
	for _, step := range steps {
		if t.Failed() {
			break
		}
		step.run(t)
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
	return tc.stack[len(tc.stack)-1]
}

func (tc *uiTestCompiler) add(step uiTestStep) {
	if i := len(tc.stack) - 1; i >= 0 {
		tc.stack[i].uiTestStep = appendTestStep(tc.stack[i].uiTestStep, step)
	} else {
		tc.stack = append(tc.stack, named{uiTestStep: step})
	}
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
