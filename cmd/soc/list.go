package main

import (
	"errors"
	"fmt"

	"github.com/jcorbin/soc/internal/socui"
)

func init() {
	builtinServer("list", serveList,
		"print stream outline listing")
}

func serveList(ctx context, _ *socui.Request, resp *socui.Response) (rerr error) {
	rc, err := ctx.store.open()
	if errors.Is(err, errStoreNotExists) {
		return fmt.Errorf("%w; run `soc init` to create one", err)
	} else if err != nil {
		return err
	}
	defer func() {
		if cerr := rc.Close(); rerr == nil {
			rerr = cerr
		}
	}()

	var sc outlineScanner
	sc.Reset(rc)

	n := 0
	for sc.Scan() {
		if !sc.titled {
			continue
		}
		if sc.lastTime().Grain() == 0 {
			continue
		}
		if _, isH1 := sc.heading(1); !isH1 {
			continue
		}
		n++
		fmt.Fprintf(resp, "%v. %v\n", n, sc)
	}

	return nil
}
