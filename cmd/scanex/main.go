package main

import (
	"bufio"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/jcorbin/soc/scandown"
	"github.com/jcorbin/soc/internal/socutil"
)

func main() {
	var (
		in      = os.Stdin
		out     = &socutil.ErrWriter{Writer: os.Stdout}
		verbose bool
	)

	flag.BoolVar(&verbose, "v", false, "enable verbose output")
	flag.Parse()

	logOut := socutil.PrefixWriter("> log: ", out)
	defer logOut.Close()
	log.SetOutput(logOut)
	log.SetFlags(0)

	addLogPrefix := func(prefix string) func() {
		old := logOut.Prefix
		logOut.Prefix = old + prefix
		return func() {
			logOut.Prefix = old
		}
	}

	var blocks scandown.BlockStack

	sc := bufio.NewScanner(in)
	sc.Split(blocks.Scan)

	n := 0
	if err := socutil.WriteLines(out, func(w io.Writer, _ func()) bool {
		if !sc.Scan() {
			return false
		}
		n++

		width, _ := fmt.Fprintf(w, "%v. ", n)
		itemOut := socutil.PrefixWriter(strings.Repeat(" ", width), w)
		itemOut.Skip = true
		defer itemOut.Close()
		defer addLogPrefix(itemOut.Prefix)()

		if verbose {
			fmt.Fprintf(itemOut, "@%v block stack:\n%+v\n", blocks.Offset(), blocks)
		} else {
			fmt.Fprintf(itemOut, "%v\n", blocks)
		}

		if token := sc.Bytes(); len(token) > 0 {
			io.WriteString(itemOut, "```hexdump\n")
			dumper := hex.Dumper(itemOut)
			dumper.Write(token)
			dumper.Close()
			io.WriteString(itemOut, "```\n")
		}
		return true
	}); err != nil {
		log.Fatalf("write error: %v", err)
	}

	if err := sc.Err(); err != nil {
		fmt.Printf("# main scan error\n%T: %v\n", err, err)
		os.Exit(1)
	}
}
