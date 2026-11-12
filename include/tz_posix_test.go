package include

import (
	"testing"
	"time"
)

// zoneAt returns the name and the offset of the location at the given instant.
func zoneAt(location *time.Location, at time.Time) (string, int) {
	name, offset := at.In(location).Zone()
	return name, offset
}

func TestPosixLocationReadsAFixedZone(t *testing.T) {
	t.Parallel()
	// the description OpenWrt keeps for a zone that never changes: the offset is counted west
	// of UTC, so -8 is the eight hours east of it the client lives in
	for _, description := range []string{"CST-8", "<+08>-8"} {
		location := posixLocation(description, time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC))
		if location == nil {
			t.Fatalf("%q: want a location, got nil", description)
		}
		if name, offset := zoneAt(location, time.Now()); offset != 8*int(time.Hour/time.Second) {
			t.Fatalf("%q: want +08:00, got %s %d", description, name, offset)
		}
	}
	location := posixLocation("UTC0", time.Now())
	if location == nil {
		t.Fatal("UTC0: want a location, got nil")
	}
	if name, offset := zoneAt(location, time.Now()); offset != 0 || name != "UTC" {
		t.Fatalf("UTC0: want UTC, got %s %d", name, offset)
	}
}

func TestPosixLocationReadsTheDaylightRules(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		description string
		at          string
		name        string
		offset      int
	}{
		// The eastern zone of the United States: the second Sunday of March at 02:00 of the
		// standard time and the first Sunday of November at 02:00 of the daylight time.
		{"EST5EDT,M3.2.0,M11.1.0", "2026-01-15T00:00:00Z", "EST", -5},
		{"EST5EDT,M3.2.0,M11.1.0", "2026-03-08T06:59:59Z", "EST", -5},
		{"EST5EDT,M3.2.0,M11.1.0", "2026-03-08T07:00:00Z", "EDT", -4},
		{"EST5EDT,M3.2.0,M11.1.0", "2026-07-01T00:00:00Z", "EDT", -4},
		{"EST5EDT,M3.2.0,M11.1.0", "2026-11-01T05:59:59Z", "EDT", -4},
		{"EST5EDT,M3.2.0,M11.1.0", "2026-11-01T06:00:00Z", "EST", -5},
		// Central Europe: the last Sunday of March at 02:00 and the last Sunday of October at
		// 03:00, both stated in the time that runs before the change.
		{"CET-1CEST,M3.5.0,M10.5.0/3", "2026-01-15T00:00:00Z", "CET", 1},
		{"CET-1CEST,M3.5.0,M10.5.0/3", "2026-03-29T00:59:59Z", "CET", 1},
		{"CET-1CEST,M3.5.0,M10.5.0/3", "2026-03-29T01:00:00Z", "CEST", 2},
		{"CET-1CEST,M3.5.0,M10.5.0/3", "2026-07-01T00:00:00Z", "CEST", 2},
		{"CET-1CEST,M3.5.0,M10.5.0/3", "2026-10-25T00:59:59Z", "CEST", 2},
		{"CET-1CEST,M3.5.0,M10.5.0/3", "2026-10-25T01:00:00Z", "CET", 1},
		// The southern hemisphere: the period running at the beginning of the year began in
		// the year before.
		{"AEST-10AEDT,M10.1.0,M4.1.0/3", "2026-01-15T00:00:00Z", "AEDT", 11},
		{"AEST-10AEDT,M10.1.0,M4.1.0/3", "2026-04-04T15:59:59Z", "AEDT", 11},
		{"AEST-10AEDT,M10.1.0,M4.1.0/3", "2026-04-04T16:00:00Z", "AEST", 10},
		{"AEST-10AEDT,M10.1.0,M4.1.0/3", "2026-07-01T00:00:00Z", "AEST", 10},
		{"AEST-10AEDT,M10.1.0,M4.1.0/3", "2026-10-03T15:59:59Z", "AEST", 10},
		{"AEST-10AEDT,M10.1.0,M4.1.0/3", "2026-10-03T16:00:00Z", "AEDT", 11},
		// a day of the year rather than a week of a month, with the leap day it counts
		{"XXX0YYY,J60/1,J300/1", "2026-03-01T01:00:00Z", "YYY", 1},
		{"XXX0YYY,J60/1,J300/1", "2024-03-01T01:00:00Z", "YYY", 1},
	} {
		at, err := time.Parse(time.RFC3339, testCase.at)
		if err != nil {
			t.Fatalf("%s: %v", testCase.at, err)
		}
		location := posixLocation(testCase.description, at)
		if location == nil {
			t.Fatalf("%q: want a location at %s, got nil", testCase.description, testCase.at)
		}
		name, offset := zoneAt(location, at)
		if name != testCase.name || offset != testCase.offset*int(time.Hour/time.Second) {
			t.Fatalf("%q at %s: want %s %+d, got %s %+d",
				testCase.description, testCase.at, testCase.name, testCase.offset, name,
				offset/int(time.Hour/time.Second))
		}
	}
}

func TestPosixLocationRejectsWhatItDoesNotRead(t *testing.T) {
	t.Parallel()
	for _, description := range []string{"", "CST", "8", "CST-8EDT,", "CST-8EDT,M3.2.0", "CST-8EDT,M3.2.0,"} {
		if location := posixLocation(description, time.Now()); location != nil {
			t.Fatalf("%q: want nil, got %v", description, location)
		}
	}
}
