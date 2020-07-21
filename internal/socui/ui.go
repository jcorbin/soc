/* Package socui implements semi-structured user interaction paradigm for SoC.

Oriented around more-or-less free form user provided request text, and
responding in kind. The initial use case is adapting CLI args into a single
command, but the idea is to expand to other forms of interaction like: chat bot
messaging, command typed into an editor plugin prompt, or maybe even an edited
prior response.

Currently command are line delimited in the request body with space delimited
(maybe quoted) args within; TODO switch to scandown block scanning for
commands, and eventually inline content scanning before arg splitting.

*/
package socui

import (
	"bufio"
	"bytes"
	"flag"
	"io"
	"os"
	"time"

	"github.com/jcorbin/soc/internal/socutil"
)

// Handler is the interface implemented by pieces of user request handling
// logic.
type Handler interface {
	ServeUser(req *Request, resp *Response) error
}

// HandlerFunc is a functional adaptor for Handler.
type HandlerFunc func(req *Request, resp *Response) error

// ServerUser calls the receiver function pointer.
func (f HandlerFunc) ServeUser(req *Request, resp *Response) error { return f(req, resp) }

// Request represents a user request being handled, providing error tracking,
// the time of request, and input tokenization.
type Request struct {
	err  error
	now  time.Time
	body io.Reader
	cmd  *bufio.Scanner
	arg  *bufio.Scanner
}

// Response represents a response being written by a Handler.
type Response struct {
	socutil.WriteBuffer
	// TODO support structure generation, ala []scandown.Block
}

// CLIRequest builds an ArgsRequest from the current time and OS-provided args.
// Uses flag.Args() if it return non-nil.
func CLIRequest() Request {
	now := time.Now()
	args := flag.Args()
	if args == nil {
		args = os.Args[1:]
	}
	return ArgsRequest(now, args)
}

// ArgsRequest builds a Request from a given time and argument strings.
func ArgsRequest(now time.Time, args []string) Request {
	var req Request
	req.now = now
	req.body = bytes.NewReader(socutil.QuotedArgs(args))
	return req
}

// Serve runs the given handler with the receiver request and a new Response
// writing to the given writer.
// Returns any handler, request, or response error (in that order of precedence).
func (req Request) Serve(w io.Writer, handler Handler) (rerr error) {
	if err := req.err; err != nil {
		return err
	}
	defer func() {
		if rerr == nil {
			rerr = req.err
		}
	}()
	var resp Response
	resp.To = w
	defer func() {
		if ferr := resp.Flush(); rerr == nil {
			rerr = ferr
		}
	}()
	return handler.ServeUser(&req, &resp)
}

// Err returns any request scan error encountered.
func (req Request) Err() error { return req.err }

// Now returns the time user submitted the request.
func (req Request) Now() time.Time { return req.now }

// Scan scans the next user command from the body stream, preparing ScanArg state.
func (req *Request) Scan() bool {
	if req.err == nil {
		if req.cmd == nil {
			if req.cmd == nil && req.body != nil {
				req.cmd = bufio.NewScanner(req.body)
				req.cmd.Split(bufio.ScanLines) // TODO use scandown.BlockStack
			}
		}
		req.arg = nil
		if req.cmd.Scan() {
			return true
		}
		req.err = req.cmd.Err()
	}
	return false
}

// ScanArg scans the next argument within the current user command scanned from body.
func (req *Request) ScanArg() bool {
	if req.err == nil {
		if req.arg == nil {
			if req.cmd == nil && !req.Scan() {
				return false
			}
			req.arg = bufio.NewScanner(bytes.NewReader(req.cmd.Bytes())) // TODO reset-able (bytes)scanner [rescanner]
			req.arg.Split(socutil.ScanArgs)                              // TODO use [scandown inline] tokenizer before arg splitter
		}
		if req.arg.Scan() {
			return true
		}
		req.err = req.arg.Err()
	}
	return false
}

// Command returns a string containing all current bytes scanned from body.
func (req *Request) Command() string {
	if req.cmd == nil {
		return ""
	}
	return req.cmd.Text()
}

// Arg returns a string containing the current argument
func (req *Request) Arg() string {
	if req.arg == nil {
		return ""
	}
	return socutil.UnquoteArg(req.arg.Text())
}

// TODO may want to have the raw command tail, un-arg-split [rescanner]
