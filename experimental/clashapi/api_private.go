package clashapi

import (
	"net/http"

	"github.com/sagernet/sing-box/adapter"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

// privateRouter serves the endpoints of this fork, which are not part of the Clash API:
// they are meant to explain a decision rather than to configure anything, so their
// shape may change with the ranking they describe.
func privateRouter(server *Server) http.Handler {
	r := chi.NewRouter()
	r.Get("/health", getAllHealth(server))
	r.Route("/health/{name}", func(r chi.Router) {
		r.Use(parseProxyName, findProxyByName(server))
		r.Get("/", getGroupHealth(server))
	})
	return r
}

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
