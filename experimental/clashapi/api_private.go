package clashapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
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
	// The same two readings as text, for a human rather than for a parser: the report
	// above states the numbers, this one states the sentence built from them.
	r.Get("/health.txt", getAllHealthText(server))
	r.Route("/health.txt/{name}", func(r chi.Router) {
		r.Use(parseProxyName, findProxyByName(server))
		r.Get("/", getGroupHealthText(server))
	})
	return r
}
