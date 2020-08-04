package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"regexp"

	"github.com/jcorbin/soc/internal/isotime"
	"github.com/jcorbin/soc/internal/scanio"
	"github.com/jcorbin/soc/internal/socui"
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
	builtinServer("today", serveToday,
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

	return nil
}

func serveToday(ctx *context, req *socui.Request, res *socui.Response) error {
	err := ctx.today.collect(ctx.store, res)
	if err == nil {
		// display today
		res.Break()
		today := ctx.today.sections[todaySection]
		_, err = ctx.today.writeSectionsInto(res, today.byteRange)
	}
	return err
}

type presentConfig struct {
	sectionNames   []string
	sectionRemains []bool
	sectionPattern *regexp.Regexp
}

type presentDay struct {
	presentConfig
	readState
	date     isotime.GrainedTime
	sections []section
	titles   scanio.ByteTokens
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

// load reads present day section data from a stream store, retaining the
// opened reader within for further use (e.g. to actually do anything with
// section contents).
//
// It will either find a today section or a yesterday section.
// Within whichever one it finds, it then looks for the names listed in
// todaySectionNames.
func (pres *presentDay) load(st store) error {
	// close any prior storage reader
	if err := pres.Close(); err != nil {
		return err
	}

	// reset internal storage state
	{
		base := int(firstVarSection)
		pres.sections = make([]section, base, base+pres.sectionPattern.NumSubexp())
		pres.titles.Reset()
		pres.titles.Extend(base)
	}

	// open a new storage reader, and convert it for random access ala
	// io.ReaderAt, which may end up sponging the source into a tempfile if it
	// doesn't support random access
	if err := pres.open(st.open()); err != nil {
		return err
	}

	// setup the outine scanner and utilities
	var sc outlineScanner
	sc.Reset(byteRange{0, pres.size}.readerWithin(pres))

	// mark opens a new section, retaining its title bytes for later use.
	mark := func(i presentSection) {
		fmt.Fprint(&pres.titles, sc.outline)
		pres.sections[i] = sc.openSection()
		pres.titles.Set(int(i), pres.titles.Take())
	}

	// scan the stream...
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
		if i := pres.matchSection(title.Bytes()); i >= 0 {
			// allocate storage for this sub-section and then open
			j := int(firstVarSection) + i
			if n := j - len(pres.sections) + 1; n > 0 {
				pres.sections = append(pres.sections, make([]section, n)...)
				pres.titles.Extend(n)
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
	return pres.update(st, func(w io.Writer) error {
		// write the user a message on the way out
		defer func() {
			if pres.sections[yesterdaySection].id != 0 {
				log.Printf("Created Today by rolling %q forward",
					pres.titles.Get(int(yesterdaySection)).Bytes())
			} else {
				log.Printf("Created new Today section at top of stream")
			}
		}()

		// never loose a single byte of original stream content: setup to copy
		// all bytes through; the code below then will subtract processed
		// sections from this pending "copy the rest" sword
		remnants := make(byteRanges, 0, 2*len(pres.sections))
		if all := (byteRange{0, pres.size}); !all.empty() {
			remnants = append(remnants, all)
		}
		defer func() { pres.writeSectionsInto(w, remnants...) }()

		// if we found yesterday, cut stream content in half before/after its
		// head, and then copy the head
		if sec := pres.sections[yesterdaySection]; sec.id != 0 {
			head, _ := remnants[0].sub(sec.headPoint())
			pres.writeSectionsInto(w, head)
			remnants.sub(head)
		}

		// write the new today section header
		fmt.Fprintf(w, "# %v\n\n", pres.date)

		// process each today sub-section, creating or carrying it forward
		for i, name := range pres.sectionNames {
			var sec section
			if j := int(firstVarSection) + i; j < len(pres.sections) {
				sec = pres.sections[j]
			}

			if sec.id == 0 {
				// add any missing sub-sections
				fmt.Fprintf(w, "## %v\n\n", name)
			} else if !pres.sectionRemains[i] {
				// carry forward non-remnant sub-sections (e.g. TODO and WIP)
				pres.writeSectionsInto(w, sec.byteRange)
				remnants.sub(sec.byteRange)
			} else {
				// leave remnant sections behind (e.g. Done)

				// copy the yesterday sub-header into today
				header := sec.header()
				pres.writeSectionsInto(w, header)

				// elide yesterday sub-header if it would start out the remnant;
				// i.e. this erases the "## Done" header inside each day
				if !remnants[0].intersect(header).empty() || remnants[1].start == header.start {
					// TODO more configurable header elision/replacement?
					remnants.sub(header)
				}
			}

			// TODO support pulling down future items
		}

		return nil
	})
}

// update runs the given with function under a pending atomic stream update.
// The stream update is aborted on any error, cleaning up any temporary data.
// Otherwise the completed temporary data will replace the prior stream
// content, and presentDay.load() will be called to reload the newly written
// state.
func (pres *presentDay) update(st store, with func(w io.Writer) error) (rerr error) {
	if pres.ReadAtCloser == nil {
		if err := pres.load(st); err != nil && !errors.Is(err, errStoreNotExists) {
			return err
		}
	}

	cwc, err := st.update()
	if errors.Is(err, errStoreNotExists) {
		cwc, err = st.create()
	}
	if err != nil {
		return err
	}
	var ws writeState
	ws.w = cwc
	defer func() {
		if rerr == nil {
			rerr = ws.err
		}
		if rerr == nil {
			rerr = cwc.Close()
		}
		if cerr := cwc.Cleanup(); rerr == nil {
			rerr = cerr
		}
		if rerr == nil {
			rerr = pres.load(st)
		}
	}()

	return with(&ws)
}
