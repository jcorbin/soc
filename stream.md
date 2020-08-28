Status: Early Prototype, not yet viable
===

The main `soc` command is shaping up, things that work:
- a `help` system
- a `list` command that prints outline items
- a `today` command that rolls time forward and prints the current day

Next up is to implement actual TODO/WIP/Done item manipulation, which should
get us to a viable tool for daily usage.

After `soc` reaches that phase, finishing off and spinning out `scandown` can
commence. Am considering shipping a ReaderAt-backed version of the
`internal/scanio.ByteArena` along with it to ease application needs to retain
or reference.

Scandown
---

After partially implementing a proof-of-concept outline printer, became
disenchanted with all extant AST-oriented Go markdown libraries. Especially
unimpressed with the non-spec status of blackfriday, and the awkward API of
goldmark. Most (?all) of them expect an eagerly read byte slice, necessitating
things like `ioutil.ReadAll`.

Started implementing `scandown` based around these points:
- Go's standard `bufio.Scanner` can be the basis of a full pull parser
- direct implementation of [commonmark](https://spec.commonmark.org)
- split into 2 halves along the lines of commonmark's 2-phase parsing strategy;
  this enables users that have little-to-no interest in inline content, to
  minimize resources spent parsing such

The first half of `scandown` is now prototyped:
- [`scandown.BlockStack.Scan()`](scandown/block.go) implements a
  `bufio.SplitFunc`, allowing markdown block structure to be scanned from a
  stream. Memory overhead scales with document complexity ( deepest block
  structure ), but should feel constant in practice ( since most documents
  exist within a low band of complexity ).

  NOTE: in particular, memory overhead does **not** scale with file size.
- see or run [`cmd/scanex/main.go`](cmd/scanex/main.go) for example usage
- has only been manually tested/verified against this `stream.md` file

--------------------------------------------------------------------------------

# TODO

- [scandown]
  - write a package level readme and/or doc.go
  - unit test for BlockStack.Seek
  - see `block.go` for more TODOs
  - Inline parsing
  - Commonmark spec conformance tests
  - continue to clarify / refactor BlockStack core logic
  - `BlockArena` ? maybe up in internal/md or scandown/x
- [scanio]
  - [spliter] abstraction of `bufio.SplitFunc`
  - [bytescanner] drive a splitter over an in-memory `[]byte`
  - [rescanner] evolution of `bufio.Scanner` that can seek and/or have its
    reader swapped
    - paired with a [splitter] extension to notify it of the discontinuity
- ideas:
  - future/upcoming items that get automatically collected when today reaches them
  - tagged item relations, using dangling shortcut reference links, or a
    `#hashtag` extension
  - stream auto formatting (reformat markdown, line(un)wrap, etc)
  - git versioned stream storage
  - ingest/import/harvest from external sources
    - mine things like `// TODO` comments from code
    - mine items from remarks in prior done items; could use during rollover
    - mine wip/done fodder or even items from git log
  - [cmd/soc] prior command state, like the ability to reference the last
    affected item, or to reply to a disambiguation prompt

# 2020-08-27

## TODO

- [cmd/soc]
  - `readState` might start to evolve into the file-backed arena, maybe even
    implementing scanning, and eventually matriculate up into scandown
  - [config] custom trigger terms, phase, even commands?
  - [model] once matching and item triggers are done, the boundary of a
    reasonable "stream model package" may start to emerge; but may defer any
    such factoring until forced by a second toplevel entry point like [socbot]
  - [matching] prototype under the list and today commands
    - using titles retained in `outlineScanner`
    - tail args may be unmatched, used by context: e.g. to add a new item using
      matched prior structure
  - allow item triggers to be used as a header prefix like `## TODO area...`

## WIP

- [cmd/soc]
  - item type aliases, e.g. "bug" or "FIXME" for "TODO" etc
  - [triggers] for item lifecycle
    - addition: `todo/wip/done ...`
    - movement: `todo/wip/done ...` [needs: matching]
    - remove: `drop todo/wip/done ...`

## Done

- [cmd/soc]
  - [dev] percolated WIP today matching work into a coupe likely commits
  - TODO: should be able to write a test and release the matching part soon,
    after a bit of percolation from the item add WIP

# 2020-08-26

- [cmd/soc]
  - [dev] phased back into today collection dev over now-working `scanio.Editor`
- [scanio]
  - [dev] implemented and tested `Editor.Remove`
  - fixed a revealed bug in `FileArena.Reset`
  - further improvements to `Token` and `byteRange` via `Editor` dev

# 2020-08-25

- [scanio] tested and released `FileArena` and supporting `Token` work

# 2020-08-24

- [scanio]
  - [dev] started a new `Editor` and `Cursor` deal based on the today
    rollover/collection code
  - [dev] small fixes to surrounding arena code revealed by the new editor test

# 2020-08-21

- percolated some of `scanio` centered work from the past week to master

# 2020-08-19

- [cmd/soc] started working today code toward using the co-developing
  `FileArena` an `Token` updates   

# 2020-08-17

- [scanio] continued new `Arena` dev
  - sketching `FileArena` and improving `Token` along the way

# 2020-08-16

- [scanio] started developing `Arena` to support `io.ReaderAt` backing
  - impetus being to provide a better substrate for stream edits like the today
    collection update, and the about to be written item addition

# 2020-08-14

- [cmd/soc]
  - sifted through and coalesced progress since 2020-08-11
  - got new outline printer past prior tests, adding necessary temporal heading
    support

# 2020-08-13

- [cmd/soc]
  - leveled up outline printing from `list` to share with `today`: now printed
    as a numbered list with sub-lists
  - dropped brackets from outline time prefix, making it easier to reconstruct
    a "coalesced heading".
  - [dev] added support for `outlineMatch` to act as as future `outlineFilter`
  - [dev] today item service now now prints matched item(s) when all args
    match, making it a filtered `list`
  - [dev] TODO all that's left to finish item service is addition (and maybe
    movement, but may defer that for follow up)

# 2020-08-12

- [cmd/soc]
  - [dev] got today outline matching to work, with much refactoring on the way

    Added offset tracking so that result section byte ranges are valid to read
    back without later translation.

    Flattened today control flow to ease understanding and imporve correctness.

# 2020-08-11

- [scandown]
  - add `BlockStack.Reset`, allowing more reliable re-use of a stack
- [cmd/soc]
  - add user docs to `outline`, `outlineScanner`, and `section`
  - made several code improvements provoked by doc writing

# 2020-08-07

- [cmd/soc]
  - [dev] factored out outline matching structure and method, nearly ready to
    finally add items

# 2020-08-06

- [cmd/soc] minor work on today matching, mostly surrounding refactors and cleanups
- [scanio] droppepd `ByteTokens`: consumers can just keep their own
  `[]ByteArenaToken` and manage an arena, rather than continue to implement a
  slice-like container type

# 2020-08-05

- [cmd/soc]
  - [dev] continuing to work on today/todo/wip/done pattern matching, got
    partial-printfing implementation sketched
  - minor supporting refactor in outline section opening logic

# 2020-08-04

- [cmd/soc]
  - generalized outline listing and reused it in today commands rather
    than printing raw markdown
  - generalized the today server to support sub-section entry points
    (todo/wip/done commands!)
  - TODO bring back raw markdown today printing as an option

# 2020-08-02

- [cmd/soc]
  - revamped builtin setup, to allow more config-driven things
  - added item type config around the present name/remains vectors; still
    hardcoded since we have no config store reading yet

# 2020-08-01

- [cmd/soc]
  - deleted `cmd/poc` and 3rd part markdown dependencies
  - reduced `socutil` package, moving args logic into `socui`, and dropping
    unused WD walk utility
  - dropped nascent pointers toward a `soc init`, instead make things just work
  - made `presentDay` loading broadly available to all commands

# 2020-07-31

- [cmd/soc]
  - collected io plumbing out of dev today command into `store.go`
    TODO refactor the `ReaderAt`/`byteRange` side into scanio
  - released today command with the majorly revamped outline scanner
  - test and finish the today command

# 2020-07-30

- added `socutil.CopySection`: will be the basis for stream
  updating as we write bit of new content within prior
- added `socutil.WriteBuffer.Break`: eases things like "make
  sure the main content is broken off from any prior head notes"
  when writing a `socui.Response`
- added `scanio.ByteTokens.Extend`: eases initalization and
  growth use cases along side a normal slice
  - TODO consider just refactoring around a normal `[]T` rather
    than continue to implement a continer `T`
- [cmd/soc]
  - revamped test framework
    - extensible strings and line-oriented expectations, with regexp matching support
    - improved sub-test auto naming feature
  - added log state management, used to incorporate logs into a
    `socui.Response` with a prefix ( e.g. blockquote )
  - [dev] the today command now works, and is tested; many
    updates to the outline module:
    - fixed several minor bugs in section scan state
    - refactored around another `byteRange` type, this time a
      basis for `section` ranges
      - This simplified today update greatly over a `remnants
        []byteRange` that starts out containing the entire stream
        range. Processing then proceeds to copy/subtract scanned
        ranges from `remnants`, and in the end simply "copies all
        remaining bytes". This gives us a rather robust approach
        that isn't prone to easily loosing prior content.
      - TODO would like to eventually unify this with the
        `byteRange` inside `scanio`, maybe using a file-backed
        arena which could mostly replace `type readState struct`

# 2020-07-28

- [cmd/soc]
  - [dev] today rollover works
    - totally refactored around minimal range copying via `io.ReaderAt`
    - complete with a partially implemented range list
    - with a `type presentDay struct` which should help for future today interactions

# 2020-07-27

- [scandown]
  - return int64 offsets, easing use with `os` routines
  - split `BlockStack.ID()` from `BlockStack.Block()` since most uses only want
    one or the other

- [cmd/soc]
  - increased ui write chunk sizes, rather than write every line
  - [dev] iterating on store io convenience routines
  - [dev] broke outline module out of list, to ease iteration alongside today
    command
    - factored out `outlineScanner`
    - improved debug formatting
    - more general heading check function
    - added structured section offset tracking

# 2020-07-26

- [cmd/soc]
  - released the list command prototype with initial ui/testing/storage infra
  - got the ui test to a nice place over a step compiler/interpreter

# 2020-07-25

- [cmd/soc]
  - [dev] list command now wired up and working; TODO test it
  - [dev] started refactoring ui test case setup: is now reusable for the list
    command, still have a ways to go towards a proper mini interpreter for an
    integration test scenario
  - [dev] minor improvements in `main()` wiring
  - [dev] made builtin command/help registration extensible
  - [dev] collapsed description server extension into help extension, to avoid
    a needless combinatoric problem. Consider the code below:
    - the combined `rootBar` + `rootFoo` value does not implement `fooRooter`,
      despite embedding a value that does
    - a workaround is to implement combined `rootFooBar` structs, maybe
      assisted by a `withBar` constructor that checks if the passed `rooter`
      implements the `bar` extension, switching implementation if so
    - another solution is the (un)wrapper approach exhibited by the `errors`
      package
    - for soc's internal command server infrastructure's help system, neither
      seemed worthwhile, so I just subsumed descriptions into the help
      extension

    ```golang
    package main

    import "fmt"

    type rooter interface {
        root() int
    }

    type fooRooter interface {
        rooter
        foo() int
    }

    type barRooter interface {
        rooter
        bar() int
    }

    type rootFoo struct {
        rooter
        f int
    }

    type rootBar struct {
        rooter
        b int
    }

    type zaro struct{ v int }

    func (z zaro) root() int    { return z.v }
    func (rf rootFoo) foo() int { return rf.f }
    func (rb rootBar) bar() int { return rb.b }

    func main() {
        a := zaro{1}
        b := zaro{2}
        c := zaro{3}
        for i, r := range []rooter{
            a,
            b,
            c,
            rootFoo{a, 4},
            rootBar{b, 5},
            rootBar{rootFoo{c, 6}, 7},
        } {
            var rv, fv, bv int
            rv = r.root()
            if f, ok := r.(fooRooter); ok {
                fv = f.foo()
            }
            if b, ok := r.(barRooter); ok {
                bv = b.bar()
            }
            fmt.Printf("[%v] r:%v f:%v b:%v\n", i, rv, fv, bv)
        }
    }
    ```

# 2020-07-24

- [cmd/soc]
  - [dev] memory/fs-based storage module with tests
  - [dev] ui module done and tested
    - command dispatcher system based around a `req`uest command/arg scanner,
      and a `res`ponse write buffer
    - [idea] eventually `req` may support structured markdown commands, easier
      to type in the chat or editor scenario; this should allow things like
      commands taking a body, or the user filling out a reply to a prior
      response
    - [idea] eventually `res` may support structured markdown output
      - can be done by using a `scandown.BlockStack` to prefix outgoing lines
        with necessary block markers
      - which could be wrapped into an `io.Writer` that writes contents within
        a block stack context
      - which could pair with an outer `io.Writer` that parses the written byte
        stream, updating the block stack as it goes, which could allow users to
        both "just write bytes" at the outer level, and also to handnoff an
        inner content writer to code that isn't structure aware

# 2020-07-22

- [scanio] started an `internal/scanio` package
  - [dev] started out with an abstracted scan/token-copy loop
  - [dev] then added a byte arena to cache tokens
  - [idea] makes room to introduce a [rescanner] fork of `bufio.Scanner`
    eventually to support things like reseting and seeking
- [cmd/soc] 
  - [dev] sketched command/server structure; next: finish help system and test
  - [dev] working list outline prototype
- support multiple commands from the CLI args stream, framed by the classic
  `--` terminator all `getopt(1)`
- lowered the isotime parsing interface to bytes for efficiency

# 2020-07-21

- wind down `cmd/poc`
  - factored out `internal/socui` around a trio of `Request` `Response` and
    `Handler` types, dropping the "server" conceit, decoupling from storage
    concerns, and now with a test

# 2020-07-20

- minor refactoring and planning towards [cmd/soc]

# 2020-07-18

- [scandown]
  - wrote initial pass at block content trimming; `BlockStack` is now also an
    `io.Reader` and an `io.Seeker`

# 2020-07-17

- broke out a readme, cleaned up the stream, and push to github
- [scandown]
  - de-interned so that it can be used from go playground
  - wrote a [playground example](https://play.golang.org/p/dBrrhPHpKWN)
    demonstrating block stack scanning
  - ratcheted up code documentation for initial publishing
  - fixed many bugs, and otherwise improved code while writing docs and
    examples

# 2020-07-16

- [scandown] BlockStack Works™ ( for me, in one-off verification )
  - enough to unblock prototyping a soc outline scanner and maybe transforms
  - enough to start work on markdown phase 2 inline parsing

# 2020-07-03

- further refactored poc command structure to be a req/res handling scheme
- initial cut at file storage

# 2020-07-01

- refactor buffer write walk helpers

# 2020-06-30

- started factoring out poc ui structures

# 2020-06-28

- wrote a poc markdown renderer for blackfriday v2
  - upstream [doesn't yet support](https://github.com/russross/blackfriday/issues/670)
  - just going with a node walker, since the `blackfriday.Renderer` doesn't
    actually seem that useful:
    - it must pass a writer along, forestalling the sort of buffer-flush
      pattern I prefer
    - once again I wish I could extend the unexported `blackfriday.nodeWalker`
  - will likely switch to goldmark after poc, altho [it also lacks a renderer](https://github.com/yuin/goldmark/issues/142)

# 2020-06-26

- while researching markdown rendering, found yuin/goldmark via
  shurcooL/markdownfmt issue; decided not to directly use either:
  - goldmark doesn't look worth the rent, and has very heavy OOP-y feel
  - markdownfmt's renderer isn't blackfriday v2 compatible
  - ... porting it doesn't seem easy or worthwhile
- prototyped today rollover, only able to see it through outline printing tho
- decided to just go with blackfriday's AST as the data model for now, rather
  than map to/from some novel domain structures

# 2020-06-23

- poc: outline walker, with minimal iso time parsing
- decided to go with blackfriday
  - v2 has better api than v1
  - dislike gomarkdown eface usage

# 2020-06-22

- poc: started to learn blackfriday node walking
- init project and stream

Test
===

This section contains sample markdown text to serve as a test case for
`scandown` and `"cmd/poc".markdownWriter`.

Because reasons

How about:

- a list item
  with hanging indent

What *is* **this** ~~anyhow~~?

Let's try some code:

```golang
package main

import "fmt"

func main() {
  fmt.Printf("Hello World!\n")
}
```

How does \
this work?

Pretty ![ooo](picture.img)

Cats ![cats][cats]

Go [here](google.com) or [there][that]

> Help!

Such <u>stuff</u>

<div>
  much
</div>

bla bla

[that]: such.com
[cats]: cats.gif
