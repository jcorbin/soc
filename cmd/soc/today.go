package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"regexp"
	"time"

	"github.com/jcorbin/soc/internal/isotime"
	"github.com/jcorbin/soc/internal/scanio"
	"github.com/jcorbin/soc/internal/socui"
	"github.com/jcorbin/soc/internal/socutil"
)

func init() {
	builtinServer("today", serveToday,
		"print today's section of the stream")
}

func serveToday(ctx context, req *socui.Request, res *socui.Response) (rerr error) {
	var pres presentDay
	defer func() {
		if cerr := pres.Close(); rerr == nil {
			rerr = cerr
		}
	}()

	if err := pres.collect(ctx.store, req.Now(), res); err != nil {
		return err
	}

	// display today
	// TODO if pres.load above could be hinted to retain all today bytes in an
	// arena, we could avoid these read(s) and buffer alloc
	res.Break()
	today := pres.sections[todaySection]
	_, err := socutil.CopySection(res, pres.src, today.start, today.end-today.start, nil)
	return err
}

type presentDay struct {
	readState
	today    isotime.GrainedTime
	sections []section
	titles   scanio.ByteTokens
}

type presentSection int

const (
	todaySection presentSection = iota
	yesterdaySection
	firstTodaySubSection
)

func (p presentSection) String() string {
	switch p {
	case todaySection:
		return "today"
	case yesterdaySection:
		return "yesterday"
	default:
		return fmt.Sprintf("today[%v]", int(p-firstTodaySubSection))
	}
}

var (
	todaySectionNames   = []string{"TODO", "WIP", "Done"}
	todaySectionRemains = []bool{false, false, true}
	todaySectionPattern *regexp.Regexp
)

func init() {
	str := `(?i:^\s*`
	for i, name := range todaySectionNames {
		name = regexp.QuoteMeta(name)
		if i > 0 {
			str += `|(` + name + `)`
		} else {
			str += `(` + name + `)`
		}
	}
	str += `\s*$)`
	todaySectionPattern = regexp.MustCompile(str)
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
	if pres.src != nil {
		err := pres.src.Close()
		pres.src, pres.srcSize = nil, 0
		if err != nil {
			return err
		}
	}

	// reset internal storage state
	{
		base := int(firstTodaySubSection)
		pres.sections = make([]section, base, base+todaySectionPattern.NumSubexp())
		pres.titles.Reset()
		pres.titles.Extend(base)
	}

	// open a new storage reader, and convert it for random access ala
	// io.ReaderAt, which may end up sponging the source into a tempfile if it
	// doesn't support random access
	{
		rc, err := st.open()
		if errors.Is(err, errStoreNotExists) {
			return nil
		}
		if err != nil {
			return err
		}
		rac, size, err := sizedReaderAt(rc)
		if err != nil {
			return err
		}
		pres.src, pres.srcSize = rac, size
	}

	// setup the outine scanner and utilities
	var sc outlineScanner
	sc.Reset(io.NewSectionReader(pres.src, 0, pres.srcSize))

	// mark opens a new section, retaining its title bytes for later use.
	mark := func(i presentSection) {
		fmt.Fprint(&pres.titles, sc.outline)
		pres.sections[i] = sc.openSection()
		pres.titles.Set(int(i), pres.titles.Take())
	}

	// markSub allocates storage for a today sub-section and then opens it.
	markSub := func(i int) {
		j := int(firstTodaySubSection) + i
		if n := j - len(pres.sections) + 1; n > 0 {
			pres.sections = append(pres.sections, make([]section, n)...)
			pres.titles.Extend(n)
		}
		mark(presentSection(j))
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
			if t.Equal(pres.today) {
				mark(todaySection)
			} else if t.Grain() == isotime.TimeGrainDay {
				if len(pres.sections) > int(firstTodaySubSection) {
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
		match := todaySectionPattern.FindSubmatchIndex(title.Bytes())
		for ii := 2; ii < len(match); {
			start := match[ii]
			ii++
			end := match[ii]
			ii++
			if start >= 0 && start < end {
				markSub(ii/2 - 2)
				break
			}
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

// collect the present, updating the stream if necessary.
//
// It first sets today from the given now, and attempts a lead.
//
// If no today section is found, an update is performed, and a new section is
// written for today.
//
// Any yesterday sub-section content is processed leaving behind remnant items
// (Done items or any unrecognized content) and carrying forward the rest
// (TODO/WIP items).
//
// Adds any sub-sections that aren't found in yesterday.
func (pres *presentDay) collect(st store, now time.Time, res *socui.Response) error {
	// try to load today, ignoring any not exists error since we can seed
	// initial stream content here
	year, month, day := now.Date()
	pres.today = isotime.Time(time.Local, year, month, day, 0, 0, 0)
	if err := pres.load(st); !errors.Is(err, errStoreNotExists) {
		if err != nil {
			return err
		} else if pres.sections[todaySection].id != 0 {
			return nil
		}
	}

	// under a pending atomic update
	return pres.update(st, func(cs *copyState) error {
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
		if all := (byteRange{0, pres.srcSize}); !all.empty() {
			remnants = append(remnants, all)
		}
		defer func() { cs.copySections(remnants...) }()

		// if we found yesterday, cut stream content in half before/after its
		// head, and then copy the head
		if sec := pres.sections[yesterdaySection]; sec.id != 0 {
			head, _ := remnants[0].sub(sec.headPoint())
			cs.copySection(head)
			remnants.sub(head)
		}

		// write the new today section header
		fmt.Fprintf(cs, "# %v\n\n", pres.today)

		// process each today sub-section, creating or carrying it forward
		for i, name := range todaySectionNames {
			var sec section
			if j := int(firstTodaySubSection) + i; j < len(pres.sections) {
				sec = pres.sections[j]
			}

			if sec.id == 0 {
				// add any missing sub-sections
				fmt.Fprintf(cs, "## %v\n\n", name)
			} else if !todaySectionRemains[i] {
				// carry forward non-remnant yesterday sub-section content (e.g. TODO and WIP)
				cs.copySection(sec.byteRange)
				remnants.sub(sec.byteRange)
			} else {
				// leave remnant content behind (e.g. Done and other day content)

				// copy the yesterday sub-header into today
				header := sec.header()
				cs.copySection(header)

				// elide yesterday sub-header if it would start out the remnant
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
func (pres *presentDay) update(st store, with func(cs *copyState) error) (rerr error) {
	if pres.src == nil {
		if err := pres.load(st); err != nil && !errors.Is(err, errStoreNotExists) {
			return err
		}
	}

	var cs copyState
	cs.readState = pres.readState

	cwc, err := st.update()
	if errors.Is(err, errStoreNotExists) {
		cwc, err = st.create()
	}
	if err != nil {
		return err
	}
	cs.w = cwc
	defer func() {
		if rerr == nil {
			rerr = cs.err
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

	return with(&cs)
}
