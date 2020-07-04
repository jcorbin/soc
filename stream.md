# Dummy

This section contains sample text in lieu of a proper unit test for
`markdownWriter` in `cmd/poc/main.go`.

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

# 2020-07-01

## TODO

- coalescing / grouping
- un-...
- last reference
- addition
- movement
- matching
- git integration
- TODO mining
  - from prior dones
  - from external code

## WIP

## Done

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
