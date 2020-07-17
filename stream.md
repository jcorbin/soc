Status: Unusable Early Prototype
===

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

Next up are two tracks in tandem:
1. start implementing `soc` in earnest, now that we have `scandown`
2. finish `scandown`: phase 2 inline parsing, an HTML renderer, and then tests
   ( leaning heavily on the commonmark spec conformance tests )

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

- scandown 
  - laminated block parsing in documentation now that it Works ™
  - ... continue to clarify / refactor BlockStack core logic
  - continuing verification, maybe proper testing Soon ™
  - start to implement phase 2 inline parsing

## Done

# 2020-07-16

- scandown BlockStack Works™ ( for me, in one-off verification )
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
