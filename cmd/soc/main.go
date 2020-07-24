package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/jcorbin/soc/internal/socui"
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
