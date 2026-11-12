package include

import (
	"os"
	"sort"
	"strings"
	"time"
)

// posixLocation returns the location a POSIX TZ description describes at at, or nil when the
// description is not one this file reads. The description is the form the system keeps: a
// standard name and its offset from UTC, optionally a daylight name and offset followed by
// the two rules its changes follow.
//
// The location is the offset in effect at at, like the one tz_android states: the rules are
// read rather than the transitions, because a location of fixed transitions would need the
// zone file this system did not install.
func posixLocation(description string, at time.Time) *time.Location {
	standardName, rest, ok := parseZoneName(description)
	if !ok {
		return nil
	}
	standard, rest, ok := parseZoneOffset(rest)
	if !ok {
		return nil
	}
	// POSIX counts the offset west of UTC, a location counts it east
	standard = -standard
	if rest == "" {
		return time.FixedZone(standardName, standard)
	}
	daylightName, rest, ok := parseZoneName(rest)
	if !ok {
		return nil
	}
	daylight := standard + int(time.Hour/time.Second)
	if rest != "" && rest[0] != ',' {
		daylight, rest, ok = parseZoneOffset(rest)
		if !ok {
			return nil
		}
		daylight = -daylight
	}
	if rest == "" {
		// a daylight name without rules states no change, so the standard time is the only
		// one the description holds
		return time.FixedZone(standardName, standard)
	}
	if rest[0] != ',' {
		return nil
	}
	start, rest, ok := parseZoneRule(rest[1:])
	if !ok || rest == "" || rest[0] != ',' {
		return nil
	}
	end, rest, ok := parseZoneRule(rest[1:])
	if !ok || rest != "" {
		return nil
	}
	if inDaylight(at, start, end, standard, daylight) {
		return time.FixedZone(daylightName, daylight)
	}
	return time.FixedZone(standardName, standard)
}

// zoneRule is one of the two days the changes of a zone follow, as the description states it.
type zoneRule struct {
	// kind is 'J' for a day of the year that never counts the leap day, 'D' for one that
	// counts it, and 'M' for a month, a week of it and a weekday.
	kind    byte
	day     int
	month   int
	week    int
	weekday int
	// time is the second of the local day the change happens at, which a rule may state
	// before the day or past it.
	time int
}

// parseZoneName reads the name a description states: the letters of an abbreviation, or
// whatever the angle brackets hold.
func parseZoneName(description string) (string, string, bool) {
	if description == "" {
		return "", description, false
	}
	if description[0] == '<' {
		end := strings.IndexByte(description, '>')
		if end < 0 {
			return "", description, false
		}
		return description[1:end], description[end+1:], true
	}
	end := 0
	for end < len(description) && (description[end] >= 'A' && description[end] <= 'Z' || description[end] >= 'a' && description[end] <= 'z') {
		end++
	}
	if end < 3 {
		return "", description, false
	}
	return description[:end], description[end:], true
}

// parseZoneOffset reads an offset stated in hours, minutes and seconds, in the direction
// POSIX states it: positive west of UTC.
func parseZoneOffset(description string) (int, string, bool) {
	sign := 1
	if description != "" && (description[0] == '+' || description[0] == '-') {
		if description[0] == '-' {
			sign = -1
		}
		description = description[1:]
	}
	end := 0
	for end < len(description) && (description[end] >= '0' && description[end] <= '9' || description[end] == ':') {
		end++
	}
	fields := strings.Split(description[:end], ":")
	if len(fields) > 3 {
		return 0, description, false
	}
	total := 0
	for index, field := range fields {
		if field == "" || len(field) > 2 {
			return 0, description, false
		}
		value := 0
		for _, digit := range field {
			if digit < '0' || digit > '9' {
				return 0, description, false
			}
			value = value*10 + int(digit-'0')
		}
		switch index {
		case 0:
			total = value * int(time.Hour/time.Second)
		case 1:
			total += value * int(time.Minute/time.Second)
		case 2:
			total += value
		}
	}
	return sign * total, description[end:], true
}

// parseZoneRule reads the day and the time a change follows.
func parseZoneRule(description string) (zoneRule, string, bool) {
	// a rule that states no time changes at 02:00 of its local day
	rule := zoneRule{time: 2 * int(time.Hour/time.Second)}
	if description == "" {
		return rule, description, false
	}
	switch description[0] {
	case 'J':
		day, rest, ok := parseRuleNumber(description[1:], 1, 365)
		if !ok {
			return rule, description, false
		}
		rule.kind, rule.day = 'J', day
		description = rest
	case 'M':
		month, rest, ok := parseRuleNumber(description[1:], 1, 12)
		if !ok || rest == "" || rest[0] != '.' {
			return rule, description, false
		}
		week, rest, ok := parseRuleNumber(rest[1:], 1, 5)
		if !ok || rest == "" || rest[0] != '.' {
			return rule, description, false
		}
		weekday, rest, ok := parseRuleNumber(rest[1:], 0, 6)
		if !ok {
			return rule, description, false
		}
		rule.kind, rule.month, rule.week, rule.weekday = 'M', month, week, weekday
		description = rest
	default:
		day, rest, ok := parseRuleNumber(description, 0, 365)
		if !ok {
			return rule, description, false
		}
		rule.kind, rule.day = 'D', day
		description = rest
	}
	if description != "" && description[0] == '/' {
		at, rest, ok := parseZoneOffset(description[1:])
		if !ok {
			return rule, description, false
		}
		rule.time = at
		description = rest
	}
	return rule, description, true
}

// parseRuleNumber reads a number in the range the rule allows.
func parseRuleNumber(description string, minimum int, maximum int) (int, string, bool) {
	end := 0
	for end < len(description) && description[end] >= '0' && description[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, description, false
	}
	value := 0
	for _, digit := range description[:end] {
		value = value*10 + int(digit-'0')
	}
	if value < minimum || value > maximum {
		return 0, description, false
	}
	return value, description[end:], true
}

// inDaylight reports whether the daylight time is the one in effect at at. The rule is stated
// in the local time that runs before the change, so the start is read in the standard time
// and the end in the daylight time. The changes of the year of at and of the one before are
// read, because the period running at the beginning of a year began in the year before in
// the southern hemisphere.
func inDaylight(at time.Time, start zoneRule, end zoneRule, standard int, daylight int) bool {
	transitions := []struct {
		at       time.Time
		daylight bool
	}{
		{ruleInstant(start, at.UTC().Year()-1, standard), true},
		{ruleInstant(end, at.UTC().Year()-1, daylight), false},
		{ruleInstant(start, at.UTC().Year(), standard), true},
		{ruleInstant(end, at.UTC().Year(), daylight), false},
	}
	sort.Slice(transitions, func(first int, second int) bool {
		return transitions[first].at.Before(transitions[second].at)
	})
	daylightInEffect := false
	for _, transition := range transitions {
		if transition.at.After(at) {
			break
		}
		daylightInEffect = transition.daylight
	}
	return daylightInEffect
}

// ruleInstant returns the instant a rule of the given year changes the zone at, the rule
// being stated in a local time of the offset that runs before the change.
func ruleInstant(rule zoneRule, year int, offset int) time.Time {
	var day time.Time
	switch rule.kind {
	case 'J':
		day = time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, rule.day-1)
		if rule.day >= 60 && isLeapYear(year) {
			// the day of the rule never counts the leap day, so a day after it is one day later
			day = day.AddDate(0, 0, 1)
		}
	case 'D':
		day = time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, rule.day)
	default:
		first := time.Date(year, time.Month(rule.month), 1, 0, 0, 0, 0, time.UTC)
		dayOfMonth := 1 + (rule.weekday-int(first.Weekday())+7)%7 + (rule.week-1)*7
		// the fifth week is the last one of the month, which may be the fourth
		lastDay := time.Date(year, time.Month(rule.month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
		for dayOfMonth > lastDay {
			dayOfMonth -= 7
		}
		day = time.Date(year, time.Month(rule.month), dayOfMonth, 0, 0, 0, 0, time.UTC)
	}
	return day.Add(time.Duration(rule.time-offset) * time.Second)
}

func isLeapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

// posixSystemLocation returns the location the system states in its POSIX description, and
// whether one was found: it answers only when Go could not read a zone of its own, which is
// the zone files the system installs and the zone at states.
func posixSystemLocation(zonefilePath string, posixPath string, at time.Time) (*time.Location, bool) {
	if _, err := os.Stat(zonefilePath); err == nil {
		return nil, false
	}
	if name, _ := at.Zone(); name != "UTC" {
		return nil, false
	}
	description, err := os.ReadFile(posixPath)
	if err != nil {
		return nil, false
	}
	location := posixLocation(strings.TrimSpace(string(description)), at)
	return location, location != nil
}
