package clashapi

import (
	"net/http"

	"github.com/sagernet/sing-box/adapter"

	"github.com/go-chi/render"
)

func getAllHealthText(server *Server) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		reports := make([]HealthTextReport, 0)
		for _, outbound := range server.outbound.Outbounds() {
			if report, loaded := healthTextReportOf(outbound); loaded {
				reports = append(reports, report)
			}
		}
		render.JSON(w, r, render.M{
			"groups": reports,
		})
	}
}

func getGroupHealthText(server *Server) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		proxy, _ := r.Context().Value(CtxKeyProxy).(adapter.Outbound)
		report, loaded := healthTextReportOf(proxy)
		if !loaded {
			render.Status(r, http.StatusNotFound)
			render.JSON(w, r, ErrNotFound)
			return
		}
		render.JSON(w, r, report)
	}
}

// healthTextReportOf returns the report of a group, loaded when the outbound is one that
// states its health as text.
func healthTextReportOf(outbound adapter.Outbound) (HealthTextReport, bool) {
	if outbound == nil {
		return HealthTextReport{}, false
	}
	reporter, isReporter := outbound.(healthTextReporter)
	if !isReporter {
		return HealthTextReport{}, false
	}
	summary, nodes := reporter.HealthText()
	if nodes == nil {
		// an empty list reads as "no node", a null as "not measured", and the group is
		// measured: it just has nothing to report yet.
		nodes = []string{}
	}
	return HealthTextReport{
		Tag:     outbound.Tag(),
		Type:    outbound.Type(),
		Summary: summary,
		Nodes:   nodes,
	}, true
}

// HealthTextReport is the health of one group as text: the line that states the
// selection, and one line per node.
//
// It says nothing HealthReport does not, and it is not meant to be parsed: what that
// report states as fields, this one states as the sentence built from them.
type HealthTextReport struct {
	Tag  string `json:"tag"`
	Type string `json:"type"`
	// Summary states the selection, e.g.
	// "quick/single, 36 nodes, tolerance 50ms, band 54ms".
	Summary string `json:"summary"`
	// Nodes is one line per node, in the order of the ranking, e.g.
	// "#21 OK [tag] LAST=172ms MED=171ms JIT=4ms DEV=157ms FAIL=0/10 SCORE=175ms".
	// The nodes the selection returned are marked with a trailing " SELECTED".
	Nodes []string `json:"nodes"`
}

// healthTextReporter is the part of a group this endpoint reads. It is declared here
// rather than in the group packages because it is the shape of this answer, not a concept
// the groups share: a group satisfies it by handing over the text it already builds.
type healthTextReporter interface {
	HealthText() (summary string, nodes []string)
}
