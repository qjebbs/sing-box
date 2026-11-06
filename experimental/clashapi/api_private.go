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
		// The same ranking over a shorter history, measured against the checks that
		// follow it: the window is priced by the objective rather than by a second copy
		// of it, so this answers where the numbers above come from, not what they are.
		r.Get("/windows", getGroupHealthWindows(server))
		// The history the window is measured against: the group keeps the window alone
		// until it is asked to keep more, which is what this asks.
		r.Put("/history", putGroupHealthHistory(server))
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
