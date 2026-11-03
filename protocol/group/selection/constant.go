package selection

// Strategies
const (
	StrategyRandom         string = "random"
	StrategyRoundrobin     string = "roundrobin"
	StrategyConsistentHash string = "consistenthash"
)

// Objectives
const (
	ObjectiveAlive     string = "alive"
	ObjectiveQualified string = "qualified"
	ObjectiveQuick     string = "quick"
	ObjectiveSteady    string = "steady"
)
