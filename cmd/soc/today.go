package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"regexp"
	"strings"

	"github.com/jcorbin/soc/internal/isotime"
	"github.com/jcorbin/soc/internal/scanio"
	"github.com/jcorbin/soc/internal/socui"
	"github.com/jcorbin/soc/scandown"
)

// ItemTypeConfig contains config for a stream item trigger word.
type ItemTypeConfig struct {
	// Name is the keyword used to mark items within the stream and as a
	// command trigger given by the user. The default config provides 3 such
	// names "TODO", "WIP", and "Done".
	//
	// May be used is a section header like `# TODO` or as an item prefix as in
	// `- TODO thing to do`.
	Name string

	// If Remains is true these sections are left behind during when we collect
	// the present day (e.g. under the `today` command). Otherwise such
	// sections are carried forward to the next day.
	//
	// NOTE only sections are subject to present day collection, while any
	// prefixed items in a remnant section will remain. I.E. any `- TODO thing`
	// notes left under the `# Done` section are not collected, but remain in
	// the past.
	Remains bool
}

func compileItemConfigs(itemTypes []ItemTypeConfig) (names []string, remains []bool, pattern *regexp.Regexp, err error) {
	names = make([]string, 0, len(itemTypes))
	remains = make([]bool, 0, len(itemTypes))
	for _, itemType := range itemTypes {
		remains = append(remains, itemType.Remains)
		names = append(names, itemType.Name)
	}

	{
		str := `(?i:^\s*`
		for i, name := range names {
			name = regexp.QuoteMeta(name)
			if i > 0 {
				str += `|(` + name + `)`
			} else {
				str += `(` + name + `)`
			}
		}
		str += `\s*$)`
		pattern, err = regexp.Compile(str)
	}

	return names, remains, pattern, err
}

func init() {
	builtinSetup("", setupToday)
	builtinServer("today", todayServer{"today", int(todaySection)},
		"print today's section of the stream")
}

func setupToday(ctx *context) (err error) {
	builtinItemTypes := []ItemTypeConfig{
		{Name: "TODO", Remains: false},
		{Name: "WIP", Remains: false},
		{Name: "Done", Remains: true},
	}

	// TODO read stored config
	ctx.today.sectionNames, ctx.today.sectionRemains, ctx.today.sectionPattern, err = compileItemConfigs(builtinItemTypes)
	if err != nil {
		return err
	}

	for i, name := range ctx.today.sectionNames {
		srv := serve(todayServer{name, int(firstVarSection) + i},
			fmt.Sprintf("show/add/move %v today items", name),
		)
		// TODO replace this lower registration hack with better/fuzzier mux matching
		if err := ctx.mux.handle(strings.ToLower(name), srv); err != nil {
			return err
		}
	}

	return nil
}

type todayServer struct {
	name  string
	index int
}

func (tod todayServer) serve(ctx *context, req *socui.Request, res *socui.Response) error {
	if err := ctx.today.collect(ctx.store, res); err != nil {
		return err
	}
	if tod.index >= len(ctx.today.sections) {
		return fmt.Errorf("unable to find %v %q section", ctx.today.date, tod.name)
	}

	// collect remaining command args to match against items, adding any
	// unmatched args as a new item
	var reqArgs []string
	for req.ScanArg() {
		reqArgs = append(reqArgs, req.Arg())
	}

	// compile arg patterns
	var patterns []*regexp.Regexp
	if len(reqArgs) > 0 {
		patterns = make([]*regexp.Regexp, len(reqArgs))
		for i, arg := range reqArgs {
			arg = strings.TrimSpace(arg)
			pattern, err := regexp.Compile(`(?i:` + regexp.QuoteMeta(arg) + `)`)
			if err != nil {
				return fmt.Errorf("unable to compile regexp for arg[%v]:%q : %w", i+1, arg, err)
			}
			patterns[i] = pattern
		}
	}

	// doMatch tries to match as many arg as possible against a section,
	// returning the match result set and any remaining args, or an error.
	doMatch := func(sec section) (match *outlineMatch, remArgs []string, err error) {
		if len(reqArgs) == 0 {
			return nil, nil, nil
		}
		match, err = matchOutline(&ctx.today, sec.body, patterns...)
		if err == nil {
			nextArg := match.maxNextArg()
			remArgs = reqArgs[nextArg:]
		}
		return match, remArgs, err
	}

	// match as many args as possible against prior items
	// TODO also match against sibling sections
	sec := ctx.today.sections[tod.index]
	match, args, err := doMatch(sec)
	if err != nil {
		return err
	}

	// add a new item based on the remaining args
	if len(args) > 0 {
		log.Printf("TODO add new %v item w/ %q", ctx.CommandHead(), args)
		return errors.New("unimplemented")
	}

	res.Break()

	fmt.Fprintf(res, "# %v\n", ctx.today.titles[tod.index])

	// print matched/added item(s)
	var (
		body   io.Reader = sec.body.readerWithin(&ctx.today)
		filter outlineFilter
	)

	if !match.empty() {
		// filter to any matched item or their children
		filter = outlineFilters(filter, outlineFilterFunc(func(out *outline) bool {
			_, n := match.matchIDs(out.id)
			return n > 0
		}))
	}

	// TODO option for raw markdown vs outline
	// raw := io.MultiReader(sec.header().readerWithin(&ctx.today), body)
	// if tod.index != int(todaySection) {
	// 	todSec := ctx.today.sections[todaySection]
	// 	raw = io.MultiReader(todSec.header().readerWithin(&ctx.today), raw)
	// }
	// _, err := io.Copy(res, raw)

	return printOutline(res, body, filter)
}

func matchOutline(ra io.ReaderAt, within byteRange, patterns ...*regexp.Regexp) (*outlineMatch, error) {
	var (
		sc    outlineScanner
		cur   outlineMatch   // the current match being scanned
		set   outlineMatch   // collected match results to return
		xlate []scanio.Token // used to copy titles during result collection
	)
	cur.offset = within.start
	for sc.Reset(within.readerWithin(ra)); sc.Scan(); {
		for i, sec := range cur.within {
			cur.within[i] = sc.updateSection(sec)
		}

		if !sc.titled {
			continue
		}

		// truncate current match after any of its items exit
		for i := 0; i < len(cur.matched); i++ {
			if i >= len(sc.id) || sc.id[i] != cur.matched[i] {
				xlate = cur.resultInto(&set, xlate)
				cur.truncate(i)
				if i < len(xlate) {
					xlate = xlate[:i]
				}
				break
			}
		}

		// only care about unmatched new items
		if len(cur.matched) >= len(sc.id) {
			continue
		}

		// try to match any remaining patterns
		nextArg := 0
		if i := len(cur.nextArg) - 1; i >= 0 {
			nextArg = cur.nextArg[i]
		}
		if nextArg >= len(patterns) {
			continue
		}

		// if we have a pattern, match it against outline head title content
		pattern := patterns[nextArg]
		if pattern == nil {
			continue
		}

		// match title, or skip
		outlineTitle := sc.title[len(sc.title)-1] // TODO scan just inline content, ignoring structure
		b, err := outlineTitle.Bytes()
		if err != nil {
			return nil, err
		}
		loc := pattern.FindIndex(b)
		if len(loc) == 0 {
			continue
		}
		nextArg++

		// add new matched outline node(s) with a newly opened section
		cur.pushPath(&sc.outline, nextArg, sc.openSection())
	}
	xlate = cur.resultInto(&set, xlate) // collect any last match

	return &set, sc.Err()
}

type outlineMatch struct {
	offset int64

	group   []int
	matched []int
	block   []scandown.Block
	title   []scanio.Token
	nextArg []int
	within  []section

	bar scanio.ByteArena
}

func (om *outlineMatch) empty() bool {
	return om == nil || len(om.group) == 0
}

func (om *outlineMatch) matchIDs(ids []int) (matchGroup, matchLen int) {
	if matchGroup = -1; om != nil && len(ids) > 0 {
		var (
			cur  = ids // unmatched remnant of ids
			curG = -1  // current group number
			ok   bool  // true if currently matching a group
		)
		for omI := 0; omI <= len(om.group); omI++ {
			omG := -1
			if omI < len(om.group) {
				omG = om.group[omI]
			}

			// new group or end
			if omG != curG {
				if ok {
					// track the best prefix match so far
					if n := len(ids) - len(cur); matchLen < n {
						matchGroup, matchLen = curG, n
					}
				}
				// (re)set current match state
				cur, curG, ok = ids, omG, omG >= 0
			}

			// skip to next group if match failed
			if !ok {
				continue
			}

			// advance cursor
			next := cur[0]
			cur = cur[1:]

			// match next id
			if ok = next == om.matched[omI]; !ok {
				continue
			}

			// stop early on a full match
			if len(cur) == 0 {
				return curG, len(ids)
			}
		}
	}
	return matchGroup, matchLen
}

func (om *outlineMatch) push(group, matched int, block scandown.Block, title scanio.Token, nextArg int, within section) {
	om.group = append(om.group, group)
	om.matched = append(om.matched, matched)
	om.block = append(om.block, block)
	om.title = append(om.title, title)
	om.nextArg = append(om.nextArg, nextArg)
	om.within = append(om.within, within)
}

func (om *outlineMatch) pushPath(out *outline, nextArg int, sec section) {
	group, priorArg, groupLen := 0, 0, 0
	if i := len(om.group) - 1; i >= 0 {
		group = om.group[i]
		priorArg = om.nextArg[i]
		for groupLen = 0; i >= 0; i-- {
			if om.group[i] != group {
				break
			}
			groupLen++
		}
	}
	for i := groupLen; i < len(out.id); i++ {
		fmt.Fprint(&om.bar, out.title[i])
		id := out.id[i]
		block := out.block[i]
		title := om.bar.Take()
		if id == sec.id {
			om.push(group, id, block, title, nextArg, sec)
			break
		} else {
			om.push(group, id, block, title, priorArg, section{})
		}
	}
}

func (om *outlineMatch) truncate(i int) {
	om.group = om.group[:i]
	om.matched = om.matched[:i]
	om.block = om.block[:i]
	om.title = om.title[:i]
	om.nextArg = om.nextArg[:i]
	om.within = om.within[:i]
	om.bar.PruneTo(om.title)
}

func (om *outlineMatch) maxNextArg() int {
	r := 0
	if om != nil {
		for _, na := range om.nextArg {
			if r < na {
				r = na
			}
		}
	}
	return r
}

func (om *outlineMatch) resultInto(dest *outlineMatch, xlate []scanio.Token) []scanio.Token {
	i := len(om.matched) - 1
	if i < 0 || om.within[i].id == 0 {
		return xlate
	}
	priorArg := dest.maxNextArg()
	if nextArg := om.nextArg[i]; nextArg < priorArg {
		return xlate
	} else if nextArg > priorArg {
		dest.truncate(0)
		xlate = xlate[:0]
	}
	return om.addInto(dest, xlate)
}

func (om *outlineMatch) addInto(dest *outlineMatch, xlate []scanio.Token) []scanio.Token {
	if len(om.group) == 0 {
		return xlate
	}

	for _, title := range om.title[len(xlate):] {
		b, _ := title.Bytes()
		dest.bar.Write(b)
		xlate = append(xlate, dest.bar.Take())
	}
	offset := om.offset - dest.offset

	var destGroup int
	if i := len(dest.group) - 1; i >= 0 {
		destGroup = dest.group[i]
		destGroup++
	}

	group := om.group[0]
	for i, g := range om.group {
		if g != group {
			destGroup++
			group = g
		}
		dest.push(destGroup, om.matched[i], om.block[i], xlate[i], om.nextArg[i], om.within[i].add(offset))
	}

	return xlate
}

type presentConfig struct {
	sectionNames   []string
	sectionRemains []bool
	sectionPattern *regexp.Regexp
}

type presentDay struct {
	presentConfig

	scanio.FileArena
	loaded   bool
	date     isotime.GrainedTime
	sections []section
	titles   []scanio.Token
	arena    scanio.ByteArena
}

type presentSection int

const (
	todaySection presentSection = iota
	yesterdaySection
	firstVarSection
)

func (p presentSection) String() string {
	switch p {
	case todaySection:
		return "today"
	case yesterdaySection:
		return "yesterday"
	default:
		return fmt.Sprintf("today[%v]", int(p-firstVarSection))
	}
}

func (pc presentConfig) matchSection(name []byte) int {
	return pc.matchSectionIndex(pc.sectionPattern.FindSubmatchIndex(name))
}

func (pc presentConfig) matchSectionString(name string) int {
	return pc.matchSectionIndex(pc.sectionPattern.FindStringSubmatchIndex(name))
}

func (pc presentConfig) matchSectionIndex(match []int) int {
	for ii := 2; ii < len(match); {
		start := match[ii]
		ii++
		end := match[ii]
		ii++
		if start >= 0 && start < end {
			return ii/2 - 2
		}
	}
	return -1
}

// open resets receiver state and (re)opens its FileArena from the given store.
func (pres *presentDay) open(st store) error {
	if err := pres.reset(); err != nil {
		return err
	}
	rc, err := st.open()
	if err != nil {
		return err
	}
	ra, size, err := sizedReaderAt(rc)
	if err != nil {
		return err
	}
	return pres.FileArena.Reset(ra, size)
}

func (pres *presentDay) reset() error {
	err := pres.Close()
	base := int(firstVarSection)
	max := base + pres.sectionPattern.NumSubexp()
	pres.sections = make([]section, base, max)
	pres.titles = make([]scanio.Token, base, max)
	pres.arena.Reset()
	pres.loaded = false
	return err
}

// load reads present day section data from a stream store, retaining the
// opened reader within for further use (e.g. to actually do anything with
// section contents).
//
// It will either find a today section or a yesterday section.
// Within whichever one it finds, it then looks for the names listed in
// todaySectionNames.
func (pres *presentDay) load(st store) (rerr error) {
	if err := pres.open(st); err != nil && !errors.Is(err, errStoreNotExists) {
		return err
	}
	defer func() {
		if rerr == nil {
			pres.loaded = true
		}
	}()

	// mark opens a new section, retaining its title bytes for later use.
	var sc outlineScanner
	mark := func(i presentSection) {
		fmt.Fprint(&pres.arena, &sc.outline)
		pres.sections[i] = sc.openSection()
		pres.titles[i] = pres.arena.Take()
	}

	// scan the stream...
	sc.Reset(scanio.Open(pres.FileArena))
	for sc.Scan() {
		// ...ending any open sections that we are no longer within
		for i, sec := range pres.sections {
			pres.sections[i] = sc.updateSection(sec)
		}

		// skip any markdown blocks that don't define an outline item title
		if !sc.titled {
			continue
		}

		// skip any outline items that a time
		t := sc.lastTime()
		if t.Grain() == 0 {
			continue
		}

		// we only care to process toplevel titles
		title, isToplevel := sc.heading(1)

		// anything with an empty title (remnant) contains only the (already
		// parsed away) time, so check for today or yesterday
		if title.Empty() {
			if t.Equal(pres.date) {
				mark(todaySection)
			} else if t.Grain() == isotime.TimeGrainDay {
				if len(pres.sections) > int(firstVarSection) {
					break
				}
				mark(yesterdaySection)
			}
			continue
		}
		if !isToplevel {
			continue
		}

		// match the item title against the recognizer pattern;
		// the group number that matches provides the sub-section index
		b, _ := title.Bytes()
		if i := pres.matchSection(b); i >= 0 {
			// allocate storage for this sub-section and then open
			j := int(firstVarSection) + i
			if n := j - len(pres.sections) + 1; n > 0 {
				pres.sections = append(pres.sections, make([]section, n)...)
				pres.titles = append(pres.titles, make([]scanio.Token, n)...)
			}
			mark(presentSection(j))
		}
	}

	// return any scanner error
	if err := sc.Err(); err != nil {
		return err
	}

	// close any still-open sections
	for i, sec := range pres.sections {
		pres.sections[i] = sc.updateSection(sec)
	}

	return nil
}

// collect performs a stream update if no today section has been found, writing
// a new today section, collecting any non-remnant yesterday content (e.g.
// TODO/WIP items), and ensuring that all today sub-sections are present.
func (pres *presentDay) collect(st store, res *socui.Response) error {
	if pres.sections[todaySection].id != 0 {
		return nil
	}
	// under a pending atomic update
	return pres.edit(st, func(ed *scanio.Editor) error {
		// write the user a message on the way out
		defer func() {
			if pres.sections[yesterdaySection].id != 0 {
				log.Printf("Created Today by rolling %q forward", pres.titles[yesterdaySection])
			} else {
				log.Printf("Created new Today section at top of stream")
			}
		}()

		// TODO run the byteRange => scanio.Token conversion up through the section struct type
		// TODO rework scanning around a FileArena
		// TODO factor out a batter scanner v2, aka scanio.Rescanner from what everges

		// if we found yesterday, cut stream content in half before/after its
		// head, and then copy the head
		cur := ed.CursorAt(0)
		if sec := pres.sections[yesterdaySection]; sec.id != 0 {
			cur.To(int(sec.start))
		}

		// write the new today section header
		fmt.Fprintf(cur, "# %v\n\n", pres.date)

		// track yesterday body remnant for potential header elision
		yesterday := pres.sections[yesterdaySection]
		remnant := scanio.MakeArea(pres.Ref(int(yesterday.body.start), int(yesterday.body.end)))
		remove := func(tok scanio.Token) scanio.Token {
			remnant.Sub(tok)
			ed.Remove(tok)
			return tok
		}

		// process each today sub-section, creating or carrying it forward
		for i, name := range pres.sectionNames {
			var sec section
			if j := int(firstVarSection) + i; j < len(pres.sections) {
				sec = pres.sections[j]
			}

			if sec.id == 0 {
				// add any missing sub-sections
				fmt.Fprintf(cur, "## %v\n\n", name)
			} else if !pres.sectionRemains[i] {
				// carry forward non-remnant sub-sections (e.g. TODO and WIP)
				cur.Insert(remove(pres.Ref(int(sec.start), int(sec.end))))
			} else {
				// leave remnant sections behind (e.g. Done)

				// elide yesterday sub-header if it would start out the day;
				// i.e. this erases the "## Done" header inside each day
				header := sec.header()
				headerTok := pres.Ref(int(header.start), int(header.end))

				if offset, headerRemains := remnant.Find(int(header.start)); headerRemains && offset == 0 {
					// TODO more configurable header elision/replacement?
					remove(headerTok)
				}

				// move or copy the yesterday sub-header into today
				cur.Insert(headerTok)
			}

			// TODO support pulling down future items
		}

		return nil
	})
}

// edit runs the given function with an editor loaded with the currently
// scanned stream's content. If with returns nil error, the editor content is
// then written out to an atomic store update. If all of that succeeds, the
// presentDay state is reloaded from the just-written stream file.
func (pres *presentDay) edit(st store, with func(ed *scanio.Editor) error) (rerr error) {
	// load if not already loaded
	if !pres.loaded {
		if err := pres.load(st); err != nil && !errors.Is(err, errStoreNotExists) {
			return err
		}
	}

	// load editor and run with
	var ed scanio.Editor
	if all := pres.RefAll(); !all.Empty() {
		ed.Append(all)
	}
	if err := with(&ed); err != nil {
		return err
	}

	// start pending atomic store update
	cwc, err := st.update()
	if errors.Is(err, errStoreNotExists) {
		cwc, err = st.create()
	}
	if err != nil {
		return err
	}
	defer func() {
		if cerr := cwc.Cleanup(); rerr == nil {
			rerr = cerr
		}
	}()

	// write editor content
	var ws writeState
	ws.w = cwc
	if _, err := ed.WriteTo(&ws); err != nil {
		return err
	}
	if err := ws.err; err != nil {
		return err
	}
	if err := cwc.Close(); err != nil {
		return err
	}

	// reload
	return pres.load(st)
}
