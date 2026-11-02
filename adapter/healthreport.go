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
// It is a diagnostic view, not a configuration: every field describes what the group
// measured and how it ranked it at the moment of the call.
type HealthReport struct {
	Tag  string `json:"tag"`
	Type string `json:"type"`
	// Objective is the objective the nodes are ranked by, Selection the whole selection,
	// e.g. "leastping" and "leastping/single".
	Objective string `json:"objective"`
	Selection string `json:"selection"`
	// Networks are the networks the nodes support.
	Networks []string `json:"networks"`
	// Generation is the number of times the cached ranking was dropped, so a value that
	// does not move between two calls means the results did not change.
	Generation uint64 `json:"generation"`
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
	// Networks are the networks this node supports.
	Networks []string `json:"networks"`
	// Selected reports whether new connections may use this node, Score is what the
	// objective ranks it by. Both are absent when the selection does not reach the node,
	// which is the case for a node of another network.
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
