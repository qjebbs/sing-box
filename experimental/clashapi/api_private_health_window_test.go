package clashapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sagernet/sing-box/adapter"
)

type stubWindowsReporter struct {
	options adapter.HealthWindowsOptions
	report  adapter.HealthWindowsReport
}

func (s *stubWindowsReporter) HealthWindows(options adapter.HealthWindowsOptions) adapter.HealthWindowsReport {
	s.options = options
	return s.report
}

func serveWindows(t *testing.T, target string, proxy any) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request = request.WithContext(context.WithValue(request.Context(), CtxKeyProxy, proxy))
	recorder := httptest.NewRecorder()
	getGroupHealthWindows(nil)(recorder, request)
	return recorder
}

func TestGroupHealthWindowsReadsTheGrid(t *testing.T) {
	t.Parallel()
	reporter := &stubWindowsReporter{
		report: adapter.HealthWindowsReport{Tag: "stub", Objective: "idle", Checks: 30, Ahead: 4},
	}
	recorder := serveWindows(t, "/private/health/stub/windows?ahead=4&configs=1:10,2:5", adapter.HealthWindowsReporter(reporter))
	if recorder.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", recorder.Code)
	}
	if reporter.options.Ahead != 4 {
		t.Fatalf("want four checks ahead, got %d", reporter.options.Ahead)
	}
	if len(reporter.options.Configs) != 2 ||
		reporter.options.Configs[0] != (adapter.HealthWindowConfig{Stride: 1, Samples: 10}) ||
		reporter.options.Configs[1] != (adapter.HealthWindowConfig{Stride: 2, Samples: 5}) {
		t.Fatalf("want the grid of the query, got %v", reporter.options.Configs)
	}
	var report adapter.HealthWindowsReport
	if err := json.Unmarshal(recorder.Body.Bytes(), &report); err != nil {
		t.Fatalf("the report is not json: %v", err)
	}
	if report.Tag != "stub" || report.Checks != 30 {
		t.Fatalf("want the report of the group, got %v", report)
	}
}

func TestGroupHealthWindowsDefaults(t *testing.T) {
	t.Parallel()
	reporter := &stubWindowsReporter{}
	recorder := serveWindows(t, "/private/health/stub/windows", adapter.HealthWindowsReporter(reporter))
	if recorder.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", recorder.Code)
	}
	// No query is the grid the group is likely to be configured with, which the objective
	// chooses: the endpoint must not invent one.
	if reporter.options.Ahead != 0 || len(reporter.options.Configs) != 0 {
		t.Fatalf("want the default grid, got %v", reporter.options)
	}
}

func TestGroupHealthWindowsRejectsBadGrid(t *testing.T) {
	t.Parallel()
	for _, target := range []string{
		"/private/health/stub/windows?ahead=1",
		"/private/health/stub/windows?configs=10",
		"/private/health/stub/windows?configs=0:10",
		"/private/health/stub/windows?configs=1:1",
	} {
		recorder := serveWindows(t, target, adapter.HealthWindowsReporter(&stubWindowsReporter{}))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: want 400, got %d", target, recorder.Code)
		}
	}
}

func TestGroupHealthWindowsWithoutScore(t *testing.T) {
	t.Parallel()
	// A group that ranks no score has no window to measure, and says so rather than
	// answering with an empty measurement.
	recorder := serveWindows(t, "/private/health/stub/windows", "a group that ranks no score")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", recorder.Code)
	}
}
