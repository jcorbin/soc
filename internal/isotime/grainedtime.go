package isotime

import (
	"bytes"
	"time"
	"unicode"
)

// TimeGrain represents the granularity of an ISO time.
type TimeGrain uint

// TimeGrain constants, from unset zero, down to second.
const (
	TimeGrainNone TimeGrain = iota
	TimeGrainYear
	TimeGrainMonth
	TimeGrainDay
	TimeGrainHour
	TimeGrainMinute
	TimeGrainSecond
)

// GrainedTime is a variably grained ISO time range: a year, month, day, hour,
// minute, or second. Its zero value has TimeGrainNone.
type GrainedTime struct {
	grain  TimeGrain
	year   int
	month  time.Month
	day    int
	hour   int
	minute int
	second int
	loc    *time.Location
}

// Grain returns the receiver's granularity.
func (t GrainedTime) Grain() TimeGrain { return t.grain }

// Year returns the receiver's year component if it's at least TimeGrainYear,
// or zero otherwise.
func (t GrainedTime) Year() int {
	if t.grain >= TimeGrainYear {
		return t.year
	}
	return 0
}

// Day returns the receiver's month component if it's at least TimeGrainMonth,
// or zero otherwise.
func (t GrainedTime) Month() time.Month {
	if t.grain >= TimeGrainMonth {
		return t.month
	}
	return 0
}

// Day returns the receiver's day component if it's at least TimeGrainDay, or
// zero otherwise.
func (t GrainedTime) Day() int {
	if t.grain >= TimeGrainDay {
		return t.day
	}
	return 0
}

// Hour returns the receiver's hour component if it's at least TimeGrainHour,
// or zero otherwise.
func (t GrainedTime) Hour() int {
	if t.grain >= TimeGrainHour {
		return t.hour
	}
	return 0
}

// Minute returns the receiver's minute component if it's at least
// TimeGrainMinute, or zero otherwise.
func (t GrainedTime) Minute() int {
	if t.grain >= TimeGrainMinute {
		return t.minute
	}
	return 0
}

// Second returns the receiver's second component if it's at least
// TimeGrainSecond, or zero otherwise.
func (t GrainedTime) Second() int {
	if t.grain >= TimeGrainSecond {
		return t.second
	}
	return 0
}

// Location returns the receiver's time zone location.
func (t GrainedTime) Location() *time.Location { return t.loc }

// Any retruns true only if the time's grain is at least year.
func (t GrainedTime) Any() bool {
	return t.grain > TimeGrainNone
}

// Equal returns true if both times have the same granularity, and equal
// components up to that grain.
func (t GrainedTime) Equal(other GrainedTime) bool {
	if other.grain != t.grain {
		return false
	}
	switch t.grain {
	case TimeGrainSecond:
		if other.second != t.second {
			return false
		}
		fallthrough
	case TimeGrainMinute:
		if other.minute != t.minute {
			return false
		}
		fallthrough
	case TimeGrainHour:
		if other.loc.String() != t.loc.String() {
			return false
		}
		if other.hour != t.hour {
			return false
		}
		fallthrough
	case TimeGrainDay:
		if other.day != t.day {
			return false
		}
		fallthrough
	case TimeGrainMonth:
		if other.month != t.month {
			return false
		}
		fallthrough
	case TimeGrainYear:
		if other.year != t.year {
			return false
		}
	}
	return true
}

// TODO func (t GrainedTime) Contains(other GrainedTime) bool

// Time returns the standard time that is the first instant within the
// receiver's time range.
func (t GrainedTime) Time() time.Time {
	switch t.grain {
	case TimeGrainNone:
	case TimeGrainYear:
		return time.Date(t.year, 1, 1, 0, 0, 0, 0, t.loc)
	case TimeGrainMonth:
		return time.Date(t.year, t.month, 1, 0, 0, 0, 0, t.loc)
	case TimeGrainDay:
		return time.Date(t.year, t.month, t.day, 0, 0, 0, 0, t.loc)
	case TimeGrainHour:
		return time.Date(t.year, t.month, t.day, t.hour, 0, 0, 0, t.loc)
	case TimeGrainMinute:
		return time.Date(t.year, t.month, t.day, t.hour, t.minute, 0, 0, t.loc)
	case TimeGrainSecond:
		return time.Date(t.year, t.month, t.day, t.hour, t.minute, t.second, 0, t.loc)
	}
	return time.Time{}
}

// String returns an ISO time string representing the time range; only
// specifies components up to the set granularity.
func (t GrainedTime) String() string {
	tt := t.Time()
	switch t.grain {
	case TimeGrainNone:
	case TimeGrainYear:
		return tt.Format("2006")
	case TimeGrainMonth:
		return tt.Format("2006-01")
	case TimeGrainDay:
		return tt.Format("2006-01-02")
	case TimeGrainHour:
		return tt.Format("2006-01-02T15Z07")
	case TimeGrainMinute:
		return tt.Format("2006-01-02T15:04Z07")
	case TimeGrainSecond:
		return tt.Format("2006-01-02T15:04:05Z07")
	}
	return ""
}

// Parse consumes any possible components from the left of the given string,
// returning a finer grained time with additional components, the trimmed
// string remnant, and true if any such components were consumed.
func (t GrainedTime) Parse(b []byte) (sub GrainedTime, rest []byte, parsed bool) {
	if t.grain >= TimeGrainSecond {
		return t, b, false
	}
	rest = bytes.TrimLeftFunc(b, unicode.IsSpace)

	for len(rest) > 0 && t.grain < TimeGrainSecond {
		next := rest
		for len(next) > 0 && next[0] == ' ' {
			next = next[1:]
		}

		if t.grain < TimeGrainHour {
			if next[0] == '-' || next[0] == '/' {
				next = next[1:]
			}
		} else {
			if next[0] == ':' {
				next = next[1:]
			}
		}
		var num int
		i := 0
		for i < len(next) {
			c := next[i]
			if c < '0' || '9' < c {
				break
			}
			num = 10*num + int(c-'0')
			i++
		}
		if i == 0 {
			break
		}
		t = t.integrate(num)
		next = next[i:]

		rest = next
		parsed = true
	}

	return t, rest, parsed
}

// ParseString is a string version of Parse.
func (t GrainedTime) ParseString(s string) (sub GrainedTime, rest string, parsed bool) {
	var restBytes []byte
	sub, restBytes, parsed = t.Parse([]byte(s))
	return sub, string(restBytes), parsed
}

func (t GrainedTime) integrate(num int) GrainedTime {
	switch t.grain {
	case TimeGrainNone:
		t.year = num

	case TimeGrainYear:
		if num == 0 || num > 12 {
			break
		}
		t.month = time.Month(num)

	case TimeGrainMonth:
		// TODO stricter max day-of-month logic
		if num == 0 || num > 31 {
			break
		}
		t.day = num

	case TimeGrainDay:
		if num > 24 {
			break
		}
		t.hour = num

	case TimeGrainHour:
		if num > 59 {
			break
		}
		t.minute = num

	case TimeGrainMinute:
		if num > 59 {
			break
		}
		t.second = num

	}
	t.grain++
	return t
}

// Time returns a GrainedTime with the given components, stopping at the first
// that isn't usable: year, month, and day must be positive, while hour, minute
// and second must be non-negative.
// If loc is nil, time.Local is used.
func Time(loc *time.Location, year int, month time.Month, day, hour, minute, second int) (t GrainedTime) {
	t.loc = loc
	if loc == nil {
		loc = time.Local
	}
	if year > 0 {
		t.grain++
		t.year = year
		if month > 0 {
			t.grain++
			t.month = month
			if day > 0 {
				t.grain++
				t.day = day
				if hour > 0 {
					t.grain++
					t.hour = hour
					if minute > 0 {
						t.grain++
						t.minute = minute
						if second > 0 {
							t.grain++
							t.second = second
						}
					}
				}
			}
		}
	}
	// TODO normalize ala time.Time
	return t
}
