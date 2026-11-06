package clashapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/sagernet/sing-box/adapter"
)

type stubResizer struct {
	sampling uint
	extra    uint
}

func (s *stubResizer) ResizeHealthHistory(sampling uint, extra uint) adapter.HealthHistory {
	s.sampling, s.extra = sampling, extra
	return adapter.HealthHistory{Sampling: sampling, Extra: extra, Retained: sampling + extra}
}

func TestGroupHealthHistoryResizes(t *testing.T) {
	t.Parallel()
	resizer := &stubResizer{sampling: 10}
	recorder := servePrivate(t, http.MethodPut, "/private/health/stub/history?sampling=15&extra=60",
		adapter.HealthHistoryResizer(resizer), putGroupHealthHistory)
	if recorder.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", recorder.Code)
	}
	if resizer.sampling != 15 || resizer.extra != 60 {
		t.Fatalf("want fifteen checks read and sixty extra, got %v", resizer)
	}
	var applied adapter.HealthHistory
	if err := json.Unmarshal(recorder.Body.Bytes(), &applied); err != nil {
		t.Fatalf("the answer is not json: %v", err)
	}
	if applied.Retained != 75 {
		t.Fatalf("want seventy five checks retained, got %v", applied)
	}
}

func TestGroupHealthHistoryChangesOneSize(t *testing.T) {
	t.Parallel()
	// A caller may want the sampling alone to move, or the history alone: an absent sampling
	// keeps the one in use rather than resetting it, and an absent extra keeps the sampling
	// alone rather than the previous extras.
	resizer := &stubResizer{}
	recorder := servePrivate(t, http.MethodPut, "/private/health/stub/history?sampling=20",
		adapter.HealthHistoryResizer(resizer), putGroupHealthHistory)
	if recorder.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", recorder.Code)
	}
	if resizer.sampling != 20 || resizer.extra != 0 {
		t.Fatalf("want the sampling at twenty and no extras, got %v", resizer)
	}

	resizer = &stubResizer{}
	servePrivate(t, http.MethodPut, "/private/health/stub/history?extra=40",
		adapter.HealthHistoryResizer(resizer), putGroupHealthHistory)
	if resizer.sampling != 0 || resizer.extra != 40 {
		t.Fatalf("want the sampling kept and forty extras, got %v", resizer)
	}
}

func TestGroupHealthHistoryRejectsBadCount(t *testing.T) {
	t.Parallel()
	for _, target := range []string{
		"/private/health/stub/history?extra=-1",
		"/private/health/stub/history?sampling=0",
		"/private/health/stub/history?sampling=nine",
	} {
		recorder := servePrivate(t, http.MethodPut, target,
			adapter.HealthHistoryResizer(&stubResizer{}), putGroupHealthHistory)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: want 400, got %d", target, recorder.Code)
		}
	}
}

func TestGroupHealthHistoryWithoutHistory(t *testing.T) {
	t.Parallel()
	recorder := servePrivate(t, http.MethodPut, "/private/health/stub/history?extra=60",
		"a group that keeps no history", putGroupHealthHistory)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", recorder.Code)
	}
}
