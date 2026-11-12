package include

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPosixSystemLocationAnswersOnlyWhereGoCannot(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	posixPath := filepath.Join(directory, "TZ")
	if err := os.WriteFile(posixPath, []byte("CST-8\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	zonefilePath := filepath.Join(directory, "localtime")
	utc := time.Date(2026, time.November, 12, 0, 30, 0, 0, time.UTC)
	cst := time.Date(2026, time.November, 12, 8, 30, 0, 0, time.FixedZone("CST", 8*int(time.Hour/time.Second)))

	location, ok := posixSystemLocation(zonefilePath, posixPath, utc)
	if !ok {
		t.Fatal("want the description to answer where no zone file is installed")
	}
	if name, offset := zoneAt(location, utc); name != "CST" || offset != 8*int(time.Hour/time.Second) {
		t.Fatalf("want CST +08:00, got %s %d", name, offset)
	}

	// a zone Go already reads is not replaced by the description
	if location, ok = posixSystemLocation(zonefilePath, posixPath, cst); ok {
		t.Fatalf("want no location where the zone is already CST, got %v", location)
	}

	// the zone files answer for the system, whatever the description says
	if err := os.WriteFile(zonefilePath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if location, ok = posixSystemLocation(zonefilePath, posixPath, utc); ok {
		t.Fatalf("want no location where a zone file is installed, got %v", location)
	}

	// a system without either keeps the UTC Go fell back to
	if location, ok = posixSystemLocation(zonefilePath, filepath.Join(directory, "none"), utc); ok {
		t.Fatalf("want no location without a description, got %v", location)
	}
}
