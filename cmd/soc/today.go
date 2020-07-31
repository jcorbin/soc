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

// TODO configurable
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

func (pres *presentDay) load(st store) error {
	if pres.src != nil {
		err := pres.src.Close()
		pres.src, pres.srcSize = nil, 0
		if err != nil {
			return err
		}
	}

	{
		base := int(firstTodaySubSection)
		pres.sections = make([]section, base, base+todaySectionPattern.NumSubexp())
		pres.titles.Reset()
		pres.titles.Extend(base)
		pres.src, pres.srcSize = nil, 0
	}

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

	var sc outlineScanner

	mark := func(i presentSection) {
		fmt.Fprint(&pres.titles, sc.outline)
		pres.sections[i] = sc.openSection()
		pres.titles.Set(int(i), pres.titles.Take())
	}
	markSub := func(i int) {
		j := int(firstTodaySubSection) + i
		if n := j - len(pres.sections) + 1; n > 0 {
			pres.sections = append(pres.sections, make([]section, n)...)
			pres.titles.Extend(n)
		}
		mark(presentSection(j))
	}

	sc.Reset(io.NewSectionReader(pres.src, 0, pres.srcSize))
	for sc.Scan() {
		for i, sec := range pres.sections {
			pres.sections[i] = sc.updateSection(sec)
		}

		if !sc.titled {
			continue
		}

		t := sc.lastTime()
		if t.Grain() == 0 {
			continue
		}

		if title, isLast := sc.heading(1); title.Empty() {
			if t.Equal(pres.today) {
				mark(todaySection)
			} else if t.Grain() == isotime.TimeGrainDay {
				if len(pres.sections) > int(firstTodaySubSection) {
					break
				}
				mark(yesterdaySection)
			}
		} else if isLast {
			if match := todaySectionPattern.FindSubmatchIndex(title.Bytes()); len(match) > 0 {
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
		} else {
			continue
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}

	for i, sec := range pres.sections {
		pres.sections[i] = sc.updateSection(sec)
	}

	return nil
}

func (pres *presentDay) collect(st store, now time.Time, res *socui.Response) error {
	year, month, day := now.Date()
	pres.today = isotime.Time(time.Local, year, month, day, 0, 0, 0)
	if err := pres.load(st); !errors.Is(err, errStoreNotExists) {
		if err != nil {
			return err
		} else if pres.sections[todaySection].id != 0 {
			return nil
		}
	}
	return pres.update(st, func(cs *copyState) error {
		remnants := make(byteRanges, 0, 2*len(pres.sections))
		if sec := pres.sections[yesterdaySection]; sec.id != 0 {
			all := byteRange{0, pres.srcSize}
			head, tail := all.sub(sec.headPoint())
			cs.copySection(head)
			remnants = append(remnants, tail)
		}

		fmt.Fprintf(cs, "# %v\n\n", pres.today)

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

		cs.copySections(remnants...)

		if pres.sections[yesterdaySection].id != 0 {
			log.Printf("Created Today by rolling %q forward",
				pres.titles.Get(int(yesterdaySection)).Bytes())
		} else {
			log.Printf("Created new Today section at top of stream")
		}

		return nil
	})
}

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
