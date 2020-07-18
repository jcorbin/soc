package main

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/jcorbin/soc/internal/socutil"
	"github.com/jcorbin/soc/scandown"
)

func main() {
	var (
		in      = os.Stdin
		out     = &socutil.ErrWriter{Writer: os.Stdout}
		hexdump bool
		blanks  bool
		raw     bool
		verbose bool
	)

	flag.BoolVar(&blanks, "blanks", false, "print blank tokens too")
	flag.BoolVar(&hexdump, "hex", false, "hexdump content rather than quote")
	flag.BoolVar(&raw, "raw", false, "dump raw block content, don't strip marks")
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

		var body io.ReadSeeker = &blocks
		if raw {
			body = bytes.NewReader(sc.Bytes())
		} else if n, err := body.Seek(0, io.SeekEnd); err != nil {
			log.Fatalf("body seek error: %v", err)
		} else if n == 0 {
			body = nil
		} else {
			body.Seek(0, io.SeekStart)
		}

		if body != nil || blanks {
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

			if body != nil {
				if hexdump {
					io.WriteString(itemOut, "```hexdump\n")
					dumper := hex.Dumper(itemOut)
					io.Copy(dumper, body)
					dumper.Close()
					io.WriteString(itemOut, "```\n")
				} else {
					pw := socutil.PrefixWriter("> ", itemOut)
					io.Copy(pw, body)
					pw.Close()
				}
				if b := itemOut.Buffer.Bytes(); len(b) > 0 && b[len(b)-1] != '\n' {
					io.WriteString(itemOut, "\n")
				}
			}
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
