package main

import (
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/jcorbin/soc/internal/socui"
	"github.com/jcorbin/soc/internal/socutil"
)

const streamFileName = "stream.md"

func main() {
	var ui ui
	ui.args = []string{filepath.Base(os.Args[0])}

	// find stream file relative to the current directory
	{
		var fst fsStore
		wd, err := os.Getwd()
		if err != nil {
			log.Fatalf("unable to get working directory: %v", err)
		}
		fst.filename, fst.fileinfo = findFileFromWD(wd, "stream.md")

		path, err := filepath.Abs(fst.filename)
		if err != nil {
			log.Fatalf("unable to resolve abs path to %v: %v", fst.filename, err)
		}
		fst.filename = path

		ui.store = &fst
	}

	// run the user command(s)
	// TODO option for a simple REPL at least
	if err := socui.CLIRequest().Serve(os.Stdout, &ui); err != nil {
		log.Fatalln(err)
	}
}

// TODO drop socutil.FindWDFile post poc

func findFileFromWD(wd, name string) (string, os.FileInfo) {
	// TODO should we apply a limit to how far up we'll go?
	for dir := wd; len(dir) > 0; dir = filepath.Dir(dir) {
		dirFileName := filepath.Join(dir, name)
		if info, err := os.Stat(dirFileName); err == nil {
			return dirFileName, info
		}
	}
	return filepath.Join(wd, name), nil
}

var logs logState

func init() { logs.setOutput(os.Stderr) }

type logState struct {
	out   io.Writer
	flags int
}

func (st logState) restore() func() {
	return func() {
		if st.out == nil {
			st.out = os.Stderr
		}
		log.SetOutput(st.out)
		log.SetFlags(st.flags)
		logs = st
	}
}

func (st *logState) setFlags(flags int) *logState {
	log.SetFlags(flags)
	st.flags = flags
	return st
}

func (st *logState) setOutput(out io.Writer) *logState {
	log.SetOutput(out)
	st.out = out
	return st
}

func (st *logState) addPrefix(prefix string) *logState {
	return st.setOutput(socutil.PrefixWriter(prefix, st.out))
}
