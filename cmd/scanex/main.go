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
	outBuf  socutil.WriteBuffer
	scanner *bufio.Scanner
	blocks  scandown.BlockStack
)

func scan() {
	defer outBuf.Flush()

	tokenCount := 0
	for out.Err == nil && scanner.Scan() {
		// skip any blank tokens (block open/close notifications) unless flagged to not
		if !blanks {
			n, err := blocks.Seek(0, io.SeekEnd)
			if err == nil {
				_, err = blocks.Seek(0, io.SeekStart)
			}
			if err != nil {
				log.Fatalf("BlockStack.Seek failed: %v", err)
			}
			if n == 0 {
				continue
			}
		}

		// write an ordered bullet, compute hanging indent, and setup so
		// that subsequent output and logs are demarcated under it
		tokenCount++
		width, _ := fmt.Fprintf(&outBuf, "%v. ", tokenCount)
		itemOut := socutil.PrefixWriter(strings.Repeat(" ", width), &outBuf)
		itemOut.Skip = true
		defer itemOut.Close()
		defer addLogPrefix(itemOut.Prefix)()

		// write the BlockStack state, as a sub-ordered-list when running verbose
		if verbose {
			fmt.Fprintf(itemOut, "%+v\n", blocks)
		} else {
			fmt.Fprintf(itemOut, "%v\n", blocks)
		}

		// write the block token body through into a codefenced hexdump, or blockquoted otherwise
		var body io.ReadSeeker = &blocks
		if raw {
			body = bytes.NewReader(scanner.Bytes())
		}
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

		// flush output, aligned to line boundaries
		outBuf.MaybeFlush()
	}
}

func main() {
	// parse flags, then wire up input and output
	flag.Parse()

	logOut = socutil.PrefixWriter("> log: ", out)
	defer logOut.Close()
	log.SetOutput(logOut)
	log.SetFlags(0)

	outBuf.To = out

	scanner = bufio.NewScanner(in)
	scanner.Split(blocks.Scan)

	// run the main scan loop
	scan()

	// handle any errors
	if err := out.Err; err != nil {
		fmt.Printf("# write error: %v", err)
		os.Exit(1)
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
