package scandown_test

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/jcorbin/soc/scandown"
)

func Example() {
	var blocks scandown.BlockStack
	sc := bufio.NewScanner(strings.NewReader(`

# A Header

This is an initial
paragraph with content
    NOTE: internal indentation does not start a new code block

~~~somelang
I bet you didn't know we could fence code with tildes

But it's really convenient inside a Go backtick string...
~~~

A 2nd level setext heading
---

---
But that previous line was a ruler.

Now let's put things in lists:
- a thing
  with hanging indent
- an other thing
lazily continued
* different bullet, different list

  but blanks continue, when indented
1. list type change again, this time ordered
   - sub items, why not
2. a sibling ordered item
3) different delimiter, different list
   > we've not used block quotes yet, why not?

> ### Did you know that block quotes wrap all structure?
>
>     Useful, I guess, since that means it really can wrap/comment/quote anything
>     also this should be a quoted code block

`))
	sc.Split(blocks.Scan)
	for limit := 10000; sc.Scan(); limit-- {
		if limit < 0 {
			fmt.Printf("ERROR: scan loop limit exceeded\n")
			return
		}
		content, _ := ioutil.ReadAll(&blocks)
		// fmt.Printf("%q %+v\n\n", content, blocks)
		fmt.Printf("%v %q\n", blocks, content)
	}

	// Output:
	// @0 /Blank#1 ""
	// @2 /Heading1#2 "A Header"
	// @13 /Blank#3 ""
	// @14 /Paragraph#4 "This is an initial paragraph with content NOTE: internal indentation does not start a new code block"
	// @119 /Blank#5 ""
	// @120 /Codefence#6 "I bet you didn't know we could fence code with tildes\n\nBut it's really convenient inside a Go backtick string...\n"
	// @249 /Blank#7 ""
	// @250 /Heading2#9 "A 2nd level setext heading "
	// @281 /Blank#10 ""
	// @282 /Ruler#11 ""
	// @286 /Paragraph#12 "But that previous line was a ruler."
	// @322 /Blank#13 ""
	// @323 /Paragraph#14 "Now let's put things in lists:"
	// @354 List#15 ""
	// @354 List#15 Item#16 ""
	// @354 List#15 Item#16 /Paragraph#17 "a thing with hanging indent"
	// @386 List#15 /Item#16 ""
	// @386 List#15 Item#18 ""
	// @386 List#15 Item#18 /Paragraph#19 "an other thing lazily continued"
	// @420 List#15 /Item#18 ""
	// @420 /List#15 ""
	// @420 List#20 ""
	// @420 List#20 Item#21 ""
	// @420 List#20 Item#21 /Paragraph#22 "different bullet, different list"
	// @455 List#20 Item#21 /Blank#23 ""
	// @456 List#20 Item#21 /Paragraph#24 "but blanks continue, when indented"
	// @493 List#20 /Item#21 ""
	// @493 /List#20 ""
	// @493 OrderedList#25 ""
	// @493 OrderedList#25 Item#26 ""
	// @493 OrderedList#25 Item#26 /Paragraph#27 "list type change again, this time ordered"
	// @538 OrderedList#25 Item#26 List#28 ""
	// @538 OrderedList#25 Item#26 List#28 Item#29 ""
	// @538 OrderedList#25 Item#26 List#28 Item#29 /Paragraph#30 "sub items, why not"
	// @562 OrderedList#25 Item#26 List#28 /Item#29 ""
	// @562 OrderedList#25 Item#26 /List#28 ""
	// @562 OrderedList#25 /Item#26 ""
	// @562 OrderedList#25 Item#31 ""
	// @562 OrderedList#25 Item#31 /Paragraph#32 "a sibling ordered item"
	// @588 OrderedList#25 /Item#31 ""
	// @588 /OrderedList#25 ""
	// @588 OrderedList#33 ""
	// @588 OrderedList#33 Item#34 ""
	// @588 OrderedList#33 Item#34 /Paragraph#35 "different delimiter, different list"
	// @627 OrderedList#33 Item#34 Blockquote#36 ""
	// @627 OrderedList#33 Item#34 Blockquote#36 /Paragraph#37 "we've not used block quotes yet, why not?"
	// @674 OrderedList#33 Item#34 /Blockquote#36 ""
	// @674 OrderedList#33 Item#34 /Blank#38 ""
	// @675 OrderedList#33 /Item#34 ""
	// @675 /OrderedList#33 ""
	// @675 Blockquote#39 ""
	// @675 Blockquote#39 /Heading3#40 "Did you know that block quotes wrap all structure?"
	// @732 Blockquote#39 /Blank#41 ""
	// @734 Blockquote#39 /Codeblock#42 "Useful, I guess, since that means it really can wrap/comment/quote anything\nalso this should be a quoted code block\n"
	// @862 /Blockquote#39 ""
	// @862 /Blank#43 ""
}
