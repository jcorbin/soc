package main

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/jcorbin/soc/internal/isotime"
	"github.com/jcorbin/soc/internal/socui"
)

type context struct {
	args  []string
	mux   serveMux
	store store
	today presentDay
}

type server interface {
	serve(*context, *socui.Request, *socui.Response) error
}

type helpServer interface {
	server
	describe() string
	help() server
}

type serverFunc func(*context, *socui.Request, *socui.Response) error

type serverHelp struct {
	server
	d string
	h server
}

func (fn serverFunc) serve(ctx *context, req *socui.Request, res *socui.Response) error {
	return fn(ctx, req, res)
}

func (sh serverHelp) describe() string { return sh.d }
func (sh serverHelp) help() server     { return sh.h }

func textServer(text string) tmplServer {
	tmpl := template.Must(template.New("").Funcs(serverTemplateFuncs).Parse(text))
	return tmplServer{tmpl}
}

type tmplServer struct {
	tmpl *template.Template
}

func (srv tmplServer) serve(ctx *context, req *socui.Request, res *socui.Response) error {
	return srv.tmpl.Execute(res, struct {
		Ctx *context
	}{ctx})
}

type serveMux map[string]server

func (mux serveMux) handle(name string, srv server) {
	if mux[name] != nil {
		panic(fmt.Sprintf("%q server already defined", name))
	}
	mux[name] = srv
}

func (mux serveMux) handleFunc(name string, srv interface{}, args ...interface{}) {
	mux.handle(name, serve(srv, args...))
}

func (mux serveMux) helpTopic(name string, srv server) {
	topics, _ := mux[".helpTopics"].(serveMux)
	if topics[name] != nil {
		panic(fmt.Sprintf("%q topic already defined", name))
	}
	if topics == nil {
		topics = serveMux{}
		mux[".helpTopics"] = topics
	}
	topics[name] = srv
}

func (mux serveMux) helpTopicText(name string, txt string) {
	mux.helpTopic(name, textServer(txt))
}

func (mux serveMux) help() server {
	if srv := mux["help"]; srv != nil {
		return srv
	}
	return serverFunc(mux.serveHelp)
}

func (ctx context) Command() string {
	return string(socui.QuotedArgs(ctx.args))
}

func (ctx context) CommandHead() string {
	return ctx.args[len(ctx.args)-1]
}

func (ctx context) Commands() []string {
	if ctx.mux == nil {
		return nil
	}
	return ctx.mux.Commands()
}

func (ctx context) Describe(name string) string {
	if ctx.mux == nil {
		return ""
	}
	return ctx.mux.Describe(name)
}

func (ctx *context) Close() error {
	return ctx.today.Close()
}

func (mux serveMux) Commands() []string {
	var names []string
	for name := range mux {
		if name != "" {
			names = append(names, name)
		}
	}
	if mux["help"] == nil {
		names = append(names, "help")
	}
	sort.Strings(names)
	return names
}

func (mux serveMux) Describe(name string) string {
	if hs, _ := mux[name].(helpServer); hs != nil {
		return hs.describe()
	}
	if name == "help" {
		return "show help overview or on a specific topic or command"
	}
	return ""
}

func (mux serveMux) helpTopics() serveMux {
	topics, _ := mux[".helpTopics"].(serveMux)
	return topics
}

func (mux serveMux) serve(ctx *context, req *socui.Request, res *socui.Response) error {
	any := false
	for req.Scan() && req.ScanArg() {
		any = true
		if err := mux.serveCommand(ctx, req, res); err != nil {
			return err
		}
	}
	if any {
		return nil
	}
	if cmd := mux[""]; cmd != nil {
		return cmd.serve(ctx, req, res)
	}
	if cmd := mux["help"]; cmd != nil {
		return cmd.serve(ctx, req, res)
	}
	return mux.serveHelp(ctx, req, res)
}

func (mux serveMux) serveCommand(ctx *context, req *socui.Request, res *socui.Response) error {
	name := req.Arg()
	ctx.args = append(ctx.args[:len(ctx.args):len(ctx.args)], name)
	ctx.mux = mux

	cmd := mux[name]
	if cmd != nil {
		return cmd.serve(ctx, req, res)
	}
	if name == "help" {
		return mux.serveHelp(ctx, req, res)
	}
	fmt.Fprintf(res, "unrecognized command %q\n", name)
	// TODO help / feedback / advice / fuzzy match?
	return nil
}

func (mux serveMux) serveHelp(ctx *context, req *socui.Request, res *socui.Response) error {
	var name string
	if req.ScanArg() {
		name = req.Arg()
	}

	srv := mux.helpTopics()[name]
	if srv == nil {
		if hs, ok := mux[name].(helpServer); ok {
			srv = hs.help()
		}
	}
	// TODO keep going if more args and srv is a mux?

	if srv != nil {
		err := srv.serve(ctx, req, res)
		if name == "" && err == nil {
			res.WriteString("TODO avail\n\n") // TODO leave this up to the topic server
		}
		return err
	}

	if name != "" {
		fmt.Fprintf(res, "> %s %s\nno help available\n", ctx.Command(), name)
		return nil
	}

	fmt.Fprintf(res, "# Usage\n")
	if ctx.CommandHead() != "help" {
		fmt.Fprintf(res, "> %s [command args...]\n", ctx.Command())
	} else if topics := mux.helpTopics(); len(topics) > 0 {
		fmt.Fprintf(res, "> %s [topic|command]\n", ctx.Command())
		fmt.Fprintf(res, "\n## Available Help Topics\n")
		printAvail(res, topics)
	} else {
		fmt.Fprintf(res, "> %s [command]\n", ctx.Command())
	}

	fmt.Fprintf(res, "\n## Available Commands\n")
	printAvail(res, ctx)

	return nil
}

type commandList interface {
	Commands() []string
	Describe(string) string
}

var serverTemplateFuncs = template.FuncMap{
	"commandList": func(cl commandList) string {
		var sb strings.Builder
		if !printAvail(&sb, cl) {
			return ""
		}
		return sb.String()
	},
}

func printAvail(w io.Writer, cl commandList) bool {
	names := cl.Commands()
	if len(names) == 0 {
		return false
	}
	width := 0
	for _, name := range names {
		if width < len(name) {
			width = len(name)
		}
	}
	for _, name := range names {
		if name != "" {
			if desc := cl.Describe(name); desc != "" {
				fmt.Fprintf(w, "- % -*s: %s\n", width, name, desc)
			} else {
				fmt.Fprintf(w, "- %s\n", name)
			}
		}
	}
	return true
}

func serve(srv interface{}, args ...interface{}) (actual server) {
	switch val := srv.(type) {
	case server:
		actual = val
	case func(*context, *socui.Request, *socui.Response) error:
		actual = serverFunc(val)
	case string:
		actual = textServer(val)
	default:
		panic(fmt.Sprintf("unsupported serve base arg type %T", srv))
	}
	for _, arg := range args {
		switch val := arg.(type) {
		case string:
			hs, hadHelp := actual.(serverHelp)
			if !hadHelp {
				hs.server = actual
			}
			if hs.d == "" {
				hs.d = val
			} else if hs.h == nil {
				hs.h = textServer(val)
			} else {
				panic("server already has both a description and help")
			}
			actual = hs
		}
	}
	return actual
}

var builtins []func(mux serveMux)

func builtinServer(name string, srv interface{}, args ...interface{}) {
	actual := serve(srv, args...)
	builtins = append(builtins, func(mux serveMux) {
		mux.handle(name, actual)
	})
}

func builtinHelpTopic(name string, srv interface{}) {
	actual := serve(srv)
	builtins = append(builtins, func(mux serveMux) {
		mux.helpTopic(name, actual)
	})
}

// TODO builtinHelpTopic("stream")
// TODO builtinHelpTopic("matching")
// TODO some sort of better builtinServer("", ...): display a today summarya,
// an intro on first run, or maybe look for toplevel -h[elp] flags

type ui struct {
	context
}

func (ui *ui) init() error {
	if ui.store == nil {
		ui.store = &memStore{}
	}

	// TODO load user config from storage (extension?)

	if ui.mux == nil {
		ui.mux = make(serveMux)
		for _, addBuiltin := range builtins {
			addBuiltin(ui.mux)
		}
		// TODO user aliases and other customizations
	}

	return nil
}

func (ui *ui) ServeUser(req *socui.Request, res *socui.Response) (rerr error) {
	defer logs.restore()()
	logs.setOutput(res).setFlags(0)

	if ui.mux == nil {
		if err := ui.init(); err != nil {
			return err
		}
	}

	ctx := ui.context

	{
		year, month, day := req.Now().Date()
		ctx.today.date = isotime.Time(time.Local, year, month, day, 0, 0, 0)
	}
	defer func() {
		if cerr := ctx.Close(); rerr == nil {
			rerr = cerr
		}
	}()

	// try to load today, ignoring any not exists error; hereafter, a handler
	// may check ctx.today.src == nil and either error, or perform
	// initialization
	if err := ctx.today.load(ctx.store); err != nil && !errors.Is(err, errStoreNotExists) {
		return err
	}

	return ui.mux.serve(&ctx, req, res)
}
