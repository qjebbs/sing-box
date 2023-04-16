package balancer

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
	ObjectiveLeastPing string = "leastping"
	ObjectiveLeastLoad string = "leastload"
)
