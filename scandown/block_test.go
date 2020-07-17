package scandown_test

import (
	"bufio"
	"fmt"
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
		// fmt.Printf("%q %+v\n\n", sc.Bytes(), blocks)
		fmt.Printf("%v %q\n", blocks, sc.Bytes())
	}

	// Output:
	// @0 +2 "\n\n"
	// @2 /Heading1#2 "# A Header\n"
	// @13 +1 "\n"
	// @14 /Paragraph#4 "This is an initial\nparagraph with content\n    NOTE: internal indentation does not start a new code block\n"
	// @119 +1 "\n"
	// @120 /Codefence#6 "~~~somelang\nI bet you didn't know we could fence code with tildes\n\nBut it's really convenient inside a Go backtick string...\n~~~\n"
	// @249 +1 "\n"
	// @250 /Heading2#9 "A 2nd level setext heading\n---\n"
	// @281 +1 "\n"
	// @282 /Ruler#11 "---\n"
	// @286 /Paragraph#12 "But that previous line was a ruler.\n"
	// @322 +1 "\n"
	// @323 /Paragraph#14 "Now let's put things in lists:\n"
	// @354 List#15 ""
	// @354 List#15 Item#16 ""
	// @354 List#15 Item#16 /Paragraph#17 "- a thing\n  with hanging indent\n"
	// @386 List#15 /Item#16 ""
	// @386 List#15 Item#18 ""
	// @386 List#15 Item#18 /Paragraph#19 "- an other thing\nlazily continued\n"
	// @420 List#15 /Item#18 ""
	// @420 /List#15 ""
	// @420 List#20 ""
	// @420 List#20 Item#21 ""
	// @420 List#20 Item#21 /Paragraph#22 "* different bullet, different list\n"
	// @455 List#20 Item#21 +1 "\n"
	// @456 List#20 Item#21 /Paragraph#24 "  but blanks continue, when indented\n"
	// @493 List#20 /Item#21 ""
	// @493 /List#20 ""
	// @493 OrderedList#25 ""
	// @493 OrderedList#25 Item#26 ""
	// @493 OrderedList#25 Item#26 /Paragraph#27 "1. list type change again, this time ordered\n"
	// @538 OrderedList#25 Item#26 List#28 ""
	// @538 OrderedList#25 Item#26 List#28 Item#29 ""
	// @538 OrderedList#25 Item#26 List#28 Item#29 /Paragraph#30 "   - sub items, why not\n"
	// @562 OrderedList#25 Item#26 List#28 /Item#29 ""
	// @562 OrderedList#25 Item#26 /List#28 ""
	// @562 OrderedList#25 /Item#26 ""
	// @562 OrderedList#25 Item#31 ""
	// @562 OrderedList#25 Item#31 /Paragraph#32 "2. a sibling ordered item\n"
	// @588 OrderedList#25 /Item#31 ""
	// @588 /OrderedList#25 ""
	// @588 OrderedList#33 ""
	// @588 OrderedList#33 Item#34 ""
	// @588 OrderedList#33 Item#34 /Paragraph#35 "3) different delimiter, different list\n"
	// @627 OrderedList#33 Item#34 Blockquote#36 ""
	// @627 OrderedList#33 Item#34 Blockquote#36 /Paragraph#37 "   > we've not used block quotes yet, why not?\n"
	// @674 OrderedList#33 Item#34 /Blockquote#36 ""
	// @674 OrderedList#33 Item#34 /Blank#38 "\n"
	// @675 OrderedList#33 /Item#34 ""
	// @675 /OrderedList#33 ""
	// @675 Blockquote#39 ""
	// @675 Blockquote#39 /Heading3#40 "> ### Did you know that block quotes wrap all structure?\n"
	// @732 Blockquote#39 +2 ">\n"
	// @734 Blockquote#39 /Codeblock#42 ">     Useful, I guess, since that means it really can wrap/comment/quote anything\n>     also this should be a quoted code block\n"
	// @862 /Blockquote#39 ""
	// @862 /Blank#43 "\n"
}
