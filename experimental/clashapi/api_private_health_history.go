package clashapi

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/render"
	"github.com/sagernet/sing-box/adapter"
)

// putGroupHealthHistory resizes the health history of the group: how many checks the
// ranking reads, and how many are kept behind them.
//
// The extra checks are kept for measuring the window, see getGroupHealthWindows: a window
// can only be measured against the checks that follow it if the history holds more checks
// than the sampling reads, and the group keeps exactly the sampling until it is asked
// otherwise. The sampling is what the ranking reads, so changing it changes the ranking,
// which is the point of changing it here rather than in a file and a restart.
//
// Either way, what was already collected stays.
func putGroupHealthHistory(server *Server) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		resizer, isResizer := r.Context().Value(CtxKeyProxy).(adapter.HealthHistoryResizer)
		if !isResizer {
			render.Status(r, http.StatusNotFound)
			render.JSON(w, r, ErrNotFound)
			return
		}
		sampling, extra, err := parseHealthHistoryQuery(r)
		if err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, render.M{"message": err.Error()})
			return
		}
		render.JSON(w, r, resizer.ResizeHealthHistory(sampling, extra))
	}
}

// parseHealthHistoryQuery reads ?sampling=15&extra=60: how many checks the ranking reads,
// and how many are kept behind them. Both are optional, since a caller may want to change one
// and keep the other: without sampling the one in use stays, without extra the history keeps
// the sampling alone, which is what it keeps by default.
func parseHealthHistoryQuery(r *http.Request) (uint, uint, error) {
	query := r.URL.Query()
	sampling, err := parseCheckCount(query.Get("sampling"), "sampling", 1)
	if err != nil {
		return 0, 0, err
	}
	extra, err := parseCheckCount(query.Get("extra"), "extra", 0)
	if err != nil {
		return 0, 0, err
	}
	return sampling, extra, nil
}

// parseCheckCount reads a number of checks, absent meaning the caller has no opinion.
func parseCheckCount(value string, name string, least int) (uint, error) {
	if value == "" {
		return 0, nil
	}
	count, err := strconv.Atoi(value)
	if err != nil || count < least {
		return 0, fmt.Errorf("%s wants a number of checks, %d or more", name, least)
	}
	return uint(count), nil
}
