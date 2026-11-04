package clashapi

import (
	"net/http"

	"github.com/go-chi/render"
	"github.com/sagernet/sing-box/adapter"
)

func getAllHealth(server *Server) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		reports := make([]adapter.HealthReport, 0)
		for _, outbound := range server.outbound.Outbounds() {
			reporter, isReporter := outbound.(adapter.HealthReporter)
			if !isReporter {
				continue
			}
			reports = append(reports, reporter.HealthReport())
		}
		render.JSON(w, r, render.M{
			"groups": reports,
		})
	}
}

func getGroupHealth(server *Server) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		reporter, isReporter := r.Context().Value(CtxKeyProxy).(adapter.HealthReporter)
		if !isReporter {
			render.Status(r, http.StatusNotFound)
			render.JSON(w, r, ErrNotFound)
			return
		}
		render.JSON(w, r, reporter.HealthReport())
	}
}
