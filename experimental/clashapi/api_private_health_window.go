package clashapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/render"
	"github.com/sagernet/sing-box/adapter"
)

// getGroupHealthWindows serves the measurement of the window the group ranks over: how
// long that window has to be before it orders the nodes the way their coming checks do.
//
// The windows are priced by the objective that ranks the group, so this reports on the
// ranking rather than on a copy of it, see adapter.HealthWindowsReporter.
func getGroupHealthWindows(server *Server) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		reporter, isReporter := r.Context().Value(CtxKeyProxy).(adapter.HealthWindowsReporter)
		if !isReporter {
			render.Status(r, http.StatusNotFound)
			render.JSON(w, r, ErrNotFound)
			return
		}
		options, err := parseHealthWindowsQuery(r)
		if err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, render.M{"message": err.Error()})
			return
		}
		render.JSON(w, r, reporter.HealthWindows(options))
	}
}

// parseHealthWindowsQuery reads ?ahead=8&configs=1:10,2:5,3:4, the grid the report is
// measured over. Both are optional: without them the report measures the grid a group is
// likely to be configured with against the next eight checks.
func parseHealthWindowsQuery(r *http.Request) (adapter.HealthWindowsOptions, error) {
	var options adapter.HealthWindowsOptions
	query := r.URL.Query()
	if ahead := query.Get("ahead"); ahead != "" {
		value, err := strconv.Atoi(ahead)
		if err != nil || value < 2 {
			return options, fmt.Errorf("ahead wants a number of checks, at least 2")
		}
		options.Ahead = value
	}
	if configs := query.Get("configs"); configs != "" {
		for _, config := range strings.Split(configs, ",") {
			stride, samples, found := strings.Cut(config, ":")
			if !found {
				return options, fmt.Errorf("configs wants stride:samples pairs, e.g. 1:10,2:5")
			}
			strideValue, strideErr := strconv.Atoi(stride)
			samplesValue, samplesErr := strconv.Atoi(samples)
			if strideErr != nil || samplesErr != nil || strideValue < 1 || samplesValue < 2 {
				return options, fmt.Errorf("configs wants stride:samples pairs, e.g. 1:10,2:5")
			}
			options.Configs = append(options.Configs, adapter.HealthWindowConfig{
				Stride:  strideValue,
				Samples: samplesValue,
			})
		}
	}
	return options, nil
}
