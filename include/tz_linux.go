//go:build linux && !android

package include

import (
	"os"
	"time"
)

// init restores the time zone the system states when Go could not find it.
//
// Go reads $TZ and /etc/localtime, and silently falls back to UTC when neither names a zone
// it can load. Neither is the zone OpenWrt keeps in /etc/TZ, a POSIX description Go does not
// read, and /etc/localtime is only created when the time zone data is installed: a machine
// the system runs in +08:00 measured, logged and reported in UTC. The health report cuts its
// day from the wall clock of its local zone, so the floors began at the midnight of UTC,
// which is 08:00 of the client that read them.
//
// It is the same correction tz_ios and tz_android make, from the description the system
// keeps instead of from a platform API. A system that answers Go already is left alone: the
// environment names a zone Go can load, or the zone files the system installed are where Go
// reads them.
func init() {
	if description := os.Getenv("TZ"); description != "" {
		if _, err := time.LoadLocation(description); err == nil {
			return
		}
	}
	if location, ok := posixSystemLocation("/etc/localtime", "/etc/TZ", time.Now()); ok {
		time.Local = location
	}
}
