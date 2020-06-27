# Dummy

Because reasons

How about:

- a list item
  with hanging indent

# 2020-06-26

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

- writing a markdown renderer for blackfriday v2
  - upstream [doesn't yet support](https://github.com/russross/blackfriday/issues/670)
  - just going with a node walker, since the `blackfriday.Renderer` doesn't
    actually seem that useful:
    - it must pass a writer along, forestalling the sort of buffer-flush
      pattern I prefer
    - once again I wish I could extend the unexported `blackfriday.nodeWalker`

## Done

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
