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

// HealthHistoryResizer is implemented by the groups whose health history can be resized at
// runtime, so the sampling can be changed and measured without a restart and without losing
// what was already collected.
type HealthHistoryResizer interface {
	// ResizeHealthHistory sets how many checks the ranking reads and how many extras are
	// kept behind them, and reports what was applied. A sampling of zero keeps the one in
	// use, an extra of zero keeps the sampling alone.
	ResizeHealthHistory(sampling uint, extra uint) HealthHistory
}

// HealthHistory is how many checks a group keeps.
type HealthHistory struct {
	// Sampling is how many checks the ranking reads, Extra how many are kept behind them,
	// and Retained the sum. The extra checks are read by the health report only.
	Sampling uint `json:"sampling"`
	Extra    uint `json:"extra"`
	Retained uint `json:"retained"`
	// Span is how far back the kept history reaches, at the interval of the checks.
	Span string `json:"span"`
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
	// Interval is the time between two checks, read from the timestamps of the history
	// rather than from the configuration: a window is as wide as the checks in it, not as
	// wide as the configuration says.
	Interval string `json:"interval"`
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
