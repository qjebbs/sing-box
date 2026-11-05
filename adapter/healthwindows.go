package adapter

// HealthWindowsReporter is implemented by the groups whose ranking reads a score, so the
// window that ranking reads can be measured against the checks that follow it.
//
// It exists to answer one question: how long a window does this ranking need before it
// orders the nodes the way their coming checks will. It is not a configuration and not a
// second ranking: the windows are priced by the same objective that prices the live one,
// so what it reports follows the algorithm rather than describing it again.
type HealthWindowsReporter interface {
	HealthWindows(options HealthWindowsOptions) HealthWindowsReport
}

// HealthWindowsOptions is what to measure of the window.
type HealthWindowsOptions struct {
	// Ahead is how many checks a window is asked to predict, 0 for the default.
	Ahead int
	// Configs are the windows to measure, empty for the default grid.
	Configs []HealthWindowConfig
}

// HealthWindowConfig is one window: how many checks it holds, and how many are skipped
// between them.
type HealthWindowConfig struct {
	Stride  int `json:"stride"`
	Samples int `json:"samples"`
}

// HealthWindowsReport is how well each window predicts the checks that follow it.
type HealthWindowsReport struct {
	Tag       string `json:"tag"`
	Objective string `json:"objective"`
	// Sampling is the interval between two checks, read from the timestamps of the
	// history rather than from the configuration: a window is as wide as the checks in it.
	Sampling string `json:"sampling"`
	// Nodes is how many nodes were measured, Checks the longest history among them, and
	// Ahead how many checks each window was asked to predict. They are reported even when
	// no window fits, so an empty report says what it was missing.
	Nodes  int `json:"nodes"`
	Checks int `json:"checks"`
	Ahead  int `json:"ahead"`
	// Configs are the windows measured, absent when the history is too short to measure
	// any of them, in which case Note says what it was short of.
	Configs []HealthWindowResult `json:"configs"`
	Note    string               `json:"note,omitempty"`
}

// HealthWindowResult is one window, averaged over every position it was measured at.
type HealthWindowResult struct {
	Stride  int    `json:"stride"`
	Samples int    `json:"samples"`
	Span    string `json:"span"`
	Windows int    `json:"windows"`
	// Rho is the rank correlation between the score of the window and the score of the
	// checks that follow it, over the same nodes: 1 when the window orders the nodes
	// exactly as their coming checks do, 0 when it orders them at random.
	Rho float64 `json:"rho"`
	// Delta is the standard error of Rho over the positions, zero when the history only
	// fits one of them.
	Delta float64 `json:"delta"`
	// Pool is the number of nodes the selection made of the window would consider.
	Pool float64 `json:"pool"`
}
