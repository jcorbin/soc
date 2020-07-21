package socui_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	. "github.com/jcorbin/soc/internal/socui"
)

func TestArgsRequest(t *testing.T) {
	for _, tc := range []struct {
		name string
		now  time.Time
		args []string
		out  []string
		// TODO test error branches
	}{
		{
			name: "nothing",
			now:  time.Date(2020, 8, 6, 7, 5, 3, 0, time.UTC),
			out: []string{
				"now: 2020-08-06T07:05:03Z",
				"",
			},
		},

		{
			name: "some args",
			now:  time.Date(2020, 8, 6, 7, 5, 3, 0, time.UTC),
			args: []string{"hello", "john doe"},
			out: []string{
				"now: 2020-08-06T07:05:03Z",
				"",
				`1) command: "hello \"john doe\""`,
				`  1. arg: "hello"`,
				`  2. arg: "john doe"`,
				"",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			ArgsRequest(tc.now, tc.args).Serve(&out, HandlerFunc(dumpRequest))
			assert.Equal(t, tc.out, strings.Split(out.String(), "\n"), "expected output")
		})
	}
}

func dumpRequest(req *Request, resp *Response) error {
	fmt.Fprintf(resp, "now: %v\n", req.Now().Format(time.RFC3339))
	for i := 1; req.Scan(); i++ {
		fmt.Fprintf(resp, "\n%v) command: %q\n", i, req.Command())
		for j := 1; req.ScanArg(); j++ {
			fmt.Fprintf(resp, "  %v. arg: %q\n", j, req.Arg())
		}
	}
	return nil
}
