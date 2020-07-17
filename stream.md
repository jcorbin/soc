Status
===

After partially implementing a proof-of-concept outline printer, became
disenchanted with all extant AST-oriented Go markdown libraries. Especially
unimpressed with the non-spec status of blackfriday, and the awkward API of
goldmark. Most (?all) of them expect an eagerly read byte slice, necessitating
things like `ioutil.ReadAll`.

Started implementing `internal/scandown` based around these points:
- Go's standard `bufio.Scanner` can be the basis of a full pull parser
- direct implementation of [commonmark](https://spec.commonmark.org)
- split into 2 halves along the lines of commonmark's 2-phase parsing strategy;
  this enables users that have little-to-no interest in inline content, to
  minimize resources spent parsing such

The first half of `scandown` is now prototyped:
- `scandown.BlockStack.Scan()` implements a `bufio.SplitFunc`, allowing
  markdown block structure to be scanned from a stream. Memory overhead scales
  with document complexity ( deepest block structure ), but should feel
  constant in practice ( since most documents exist within a low band of
  complexity ).

  NOTE: in particular, memory overhead does **not** scale with file size.
- see or run [`cmd/scanex/main.go`](cmd/scanex/main.go) for example usage
- has only been manually tested/verified against this `stream.md` file

Next up are two tracks in tandem:
1. start implementing `soc` in earnest, now that we have `scandown`
2. finish `scandown`: phase 2 inline parsing, an HTML renderer, and then tests
   ( leaning heavily on the commonmark spec conformance tests )

What Is SoC
===

It's a TODO list but:
- on a timeline
- with WIP item actions
- with Done item reflection

SoC is a system/tool to externalize or reify your Stream Of Consciousness:
- add and remember TODO items
  - ...elaborate or edit them as time goes by
  - ...may be pinned to a future date
- track current WIP (Work In Progress) items
  - ...taking notes within them as you go
- track Done items
  - ...enabling final notes on what was done
  - ...reflection and generation of follow on TODO items
- all of this happens on a date-stamped stream of days
  - the TODO/WIP/Done distinction primarily exists only within Today
  - past days contain primarily Done items
  - future days may be sketched with planned TODO items

Not SoC
---

SoC is not an issue/ticket/work tracking system:
- those systems are for stakeholders (at best) or managers (at worst)
- SoC is for the one holding the stake
- SoC could be integrated with such a system...

SoC is not a personal wiki, knowledge-base, or zettelkasten:
- altho it could be a useful funnel/drafting-area for one
- reference content ( *cough* like what you're reading now *cough* ) can be
  intermixed with a stream ( head, tail, between days )
- a stream could be adjunct/linked to such an external brain...

Principals
---

It's just markdown, use your `$EDITOR`:
- the initial audience is programmers, who already spend a significant portion
  of their time within a text editor
- use of any `soc` tool or agent may enhance that, but cannot and should not
  replace the primacy of the user's chosen text editor

Items over time:
- the stream is fundamentally oriented around linear time
- which means that the primary focus is on the present: Today
- with secondary utilities for mining the past, and managing the future

--------------------------------------------------------------------------------

# 2020-07-17

## TODO

- TODO mining
  - from prior dones
  - from external code
- git integration
- coalescing / grouping
- un-...
- last reference
- addition
- movement
- matching: move/elaborate, add/copy
- v0 command architecture, built from poc pieces and scandown

## WIP

- internal/scandown 
  - laminated block parsing in documentation now that it Works ™
  - ... continue to clarify / refactor BlockStack core logic
  - continuing verification, maybe proper testing Soon ™
  - start to implement phase 2 inline parsing

## Done

# 2020-07-16

- internal/scandown BlockStack Works™ ( for me, in one-off verification )
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
`internal/scandown` and `"cmd/poc".markdownWriter`.

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
