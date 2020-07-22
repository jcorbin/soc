package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/jcorbin/soc/internal/socui"
)

func main() {
	var ui ui
	ui.args = []string{filepath.Base(os.Args[0])}

	if err := socui.CLIRequest().Serve(os.Stdout, &ui); err != nil {
		log.Fatalln(err)
	}
}
