package main

import (
	"errors"
	"fmt"

	"github.com/jcorbin/soc/internal/isotime"
	"github.com/jcorbin/soc/internal/socui"
)

func init() {
	builtinServer("list", serveList,
		"print stream outline listing")
}

func serveList(ctx *context, _ *socui.Request, res *socui.Response) (rerr error) {
	rc, err := ctx.store.open()
	if errors.Is(err, errStoreNotExists) {
		fmt.Fprintf(res, "stream is empty, run `%v today` to initialize\n", ctx.args[0])
		fmt.Fprintf(res, "... or just start adding items with `%v <todo|wip|done> ...`\n", ctx.args[0])
		return nil
	} else if err != nil {
		return err
	}
	defer func() {
		if cerr := rc.Close(); rerr == nil {
			rerr = cerr
		}
	}()
	return printOutline(res, rc, mustCompileOutlineFilter(isotime.TimeGrainYear, 1))
}
