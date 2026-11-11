package adapter

import "time"

// HealthReporter is implemented by the groups that pick among their nodes, so the
// health of a group can be inspected without a log line: every statistic the ranking
// reads, and the checks they were computed from.
type HealthReporter interface {
	HealthReport() HealthReport
}

// HealthReport is the health of one group.
//
// It is a diagnostic view, not a configuration: every field describes the ranking the
// group is using at the moment of the call, one round of checks at a time, and the few
// fields that come from the configuration are the ones the decision reads, not the whole
// group.
type HealthReport struct {
	Tag  string `json:"tag"`
	Type string `json:"type"`
	// Objective is the objective the nodes are ranked by, Selection the whole selection,
	// e.g. "quick" and "quick/single". The cluster the objective takes around the best
	// node is in Tolerance, MinNodes and MaxNodes.
	Objective string `json:"objective"`
	Selection string `json:"selection"`
	// Networks are the networks the nodes support.
	Networks []string `json:"networks"`
	// Generation is the number of times the cached ranking was dropped, so a value that
	// does not move between two calls means the results did not change.
	Generation uint64 `json:"generation"`
	// Best is the head of the ranking, Limit the highest score that still counts as
	// equally good as it. Every other field describes the same ranking, so the decision
	// can be replayed from one report: a node is as good as Best when its score is at
	// most Limit and it is of the class Best belongs to. Limit is absent for an
	// objective that ranks no score.
	//
	// Tolerance is the configured part of that band, in milliseconds: the difference
	// worth switching for, so the rest of Limit - score(Best) is the resolution margin the
	// measurement needs. It is the value the group is using, which is the configured one
	// or the default of its objective, and it is absent for an objective that ranks no
	// score.
	//
	// Band is the width of that band, Limit - score(Best), which is Tolerance plus the
	// margin, so the two can be told apart without subtracting. It is absent under the
	// same conditions as Limit.
	Best      string  `json:"best,omitempty"`
	Limit     *uint16 `json:"limit,omitempty"`
	Band      *uint16 `json:"band,omitempty"`
	Tolerance *uint16 `json:"tolerance,omitempty"`
	// MinNodes is the fewest nodes the cluster is extended to, MaxNodes the most it is
	// limited to, 0 for no limit, so a selection larger or smaller than the band explains
	// is accounted for.
	MinNodes int `json:"min_nodes"`
	MaxNodes int `json:"max_nodes"`
	// Providers are the sources of the nodes, with the time their content last changed.
	Providers []HealthProvider `json:"providers"`
	// Selected are the nodes new connections may use, Alive every node that is not dead,
	// in preference order where the selection orders them.
	Selected []string     `json:"selected"`
	Alive    []string     `json:"alive"`
	Nodes    []HealthNode `json:"nodes"`
}

// HealthProvider is one source of the nodes of a group.
type HealthProvider struct {
	Tag       string    `json:"tag"`
	Type      string    `json:"type"`
	UpdatedAt time.Time `json:"updated_at"`
}

// HealthNode is the health of one node, and the checks it was computed from.
//
// The delays are in milliseconds, zero meaning there is none: a failed check is kept in
// Samples as a zero delay, the same way the statistics treat it, and the fields that
// would report an absent value are left out of the JSON.
type HealthNode struct {
	Tag   string `json:"tag"`
	Index int    `json:"index"`
	// Status is the class the node is ranked in: "x" never worked, "?" not checked yet,
	// "*" alive, "OK" qualified.
	Status string `json:"status"`
	// Suspended reports that the node was checked again because its connections were
	// failing, and that the check failed too. It is suspended from the selection until a
	// check passes, which is why a node can be alive with almost every check behind it
	// having worked.
	Suspended bool `json:"suspended,omitempty"`
	// Networks are the networks this node supports.
	Networks []string `json:"networks"`
	// Selected reports whether new connections may use this node, Score is what the
	// objective ranks it by, absent for an objective that ranks no score.
	Selected bool    `json:"selected"`
	Score    *uint16 `json:"score,omitempty"`

	// All is every check in the window, Fail how many of them failed. The statistics
	// below describe the checks that succeeded.
	All  int `json:"all"`
	Fail int `json:"fail"`

	Latest        *uint16 `json:"latest,omitempty"`
	Median        *uint16 `json:"median,omitempty"`
	Average       *uint16 `json:"average,omitempty"`
	Min           *uint16 `json:"min,omitempty"`
	Max           *uint16 `json:"max,omitempty"`
	Jitter        *uint16 `json:"jitter,omitempty"`
	MeanDeviation *uint16 `json:"mean_deviation,omitempty"`
	// Floor is the fastest the node has been, the reference the idle objective measures
	// against, absent until there is a baseline.
	Floor *uint16 `json:"floor,omitempty"`
	// Floors holds one minimum delay per 30-minute slot of the local day of the client, the
	// first one at its local midnight. A zero means the slot has no measurement; absent when
	// no baseline is available.
	Floors []uint16 `json:"floors,omitempty"`
	// Bad is how many of the checks came out slower than twice the floor, the frequency
	// the idle objective counts next to the depth.
	Bad *int `json:"bad,omitempty"`
	// Pressure is how much longer the node waits than at its floor, as a share of it, and
	// BadShare the share of the checks that were bad: the two terms of the idle score,
	// reported so that a score can be read back into what it was made of.
	Pressure *float32 `json:"pressure,omitempty"`
	BadShare *float32 `json:"bad_share,omitempty"`
	// FailureRate is how often the node has failed over the last few hours, which outlives
	// the window: it is the prior a clean window is read with, see healthcheck.Stats.
	FailureRate *float32 `json:"failure_rate,omitempty"`
	// FailureShare is the share of attempts the score priced as failed: the window's own
	// share or FailureRate, whichever is larger.
	FailureShare *float32 `json:"failure_share,omitempty"`
	// Spread and DeviationSpread are the sample spreads of the checks and of their
	// distances to the median, which the standard error of a score is computed from.
	Spread          *uint16 `json:"spread,omitempty"`
	DeviationSpread *uint16 `json:"deviation_spread,omitempty"`
	// Expires is when the oldest check of the window stops counting.
	Expires *time.Time `json:"expires,omitempty"`

	// Samples are the checks the statistics were computed from, most recent first.
	Samples []HealthSample `json:"samples"`
}

// HealthSample is one check of a node.
type HealthSample struct {
	Time time.Time `json:"time"`
	// Delay is the round trip of the check in milliseconds, zero when it failed.
	Delay uint16 `json:"delay"`
}
