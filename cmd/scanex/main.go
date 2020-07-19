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

var (
	in      = os.Stdin
	out     = &socutil.ErrWriter{Writer: os.Stdout}
	hexdump bool
	blanks  bool
	raw     bool
	verbose bool

	logOut  *socutil.Prefixer
	scanner *bufio.Scanner
	blocks  scandown.BlockStack
)

func main() {
	flag.Parse()

	logOut = socutil.PrefixWriter("> log: ", out)
	defer logOut.Close()
	log.SetOutput(logOut)
	log.SetFlags(0)

	scanner = bufio.NewScanner(in)
	scanner.Split(blocks.Scan)

	tokenCount := 0
	if err := socutil.WriteLines(out, func(w io.Writer, _ func()) bool {
		if !scanner.Scan() {
			return false
		}

		var body io.ReadSeeker = &blocks
		if raw {
			body = bytes.NewReader(scanner.Bytes())
		} else if n, err := body.Seek(0, io.SeekEnd); err != nil {
			log.Fatalf("body seek error: %v", err)
		} else if n == 0 {
			body = nil
		} else {
			body.Seek(0, io.SeekStart)
		}

		if body != nil || blanks {
			tokenCount++
			width, _ := fmt.Fprintf(w, "%v. ", tokenCount)
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
	if err := scanner.Err(); err != nil {
		fmt.Printf("# main scan error\n%T: %v\n", err, err)
		os.Exit(1)
	}
}

func init() {
	flag.BoolVar(&blanks, "blanks", false, "print blank tokens too")
	flag.BoolVar(&hexdump, "hex", false, "hexdump content rather than quote")
	flag.BoolVar(&raw, "raw", false, "dump raw block content, don't strip marks")
	flag.BoolVar(&verbose, "v", false, "enable verbose output")
}

func addLogPrefix(prefix string) func() {
	old := logOut.Prefix
	logOut.Prefix = old + prefix
	return func() {
		logOut.Prefix = old
	}
}
