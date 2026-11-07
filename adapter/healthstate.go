package adapter

import "time"

// HealthState is what a health check keeps across rounds, the part the sampling window does
// not hold: the floor of the node and how often it has failed over the last few hours.
//
// It is what a cache file stores per node, so the two estimates survive a restart. A zero
// field means there is nothing to keep: a node whose floor was never measured, or whose
// failures are older than the estimate remembers.
type HealthState struct {
	// Floors holds one value per slot of the day, in milliseconds like a sample, in the order
	// the check indexes them. A slot the node has not been measured in holds zero, which no
	// delay is, so the values need no flag next to them.
	Floors []uint16 `json:"floors,omitempty"`
	// FloorsAt is the window of the day the values were read in, as the start of that window:
	// not the window of the fastest slot, and not the moment each slot was written. The position
	// of a value fixes the window of the day it belongs to, and the windows are cut from the
	// wall clock, so this one window places every value: a slot is the one of the window n
	// windows before this one, and a slot further back than a day is a slot the run did not
	// measure.
	FloorsAt time.Time `json:"floors_at,omitzero"`

	// FailureSum is the weighted number of failed checks, FailureWeight the weighted number
	// of all of them, and FailureAt when the estimate was last brought forward. Their ratio
	// is the long-run failure rate, see healthcheck.failureRate.
	FailureSum    float64   `json:"failure_sum,omitzero"`
	FailureWeight float64   `json:"failure_weight,omitzero"`
	FailureAt     time.Time `json:"failure_at,omitzero"`
}

// HealthStateStore is implemented by a cache file that keeps HealthState across restarts.
//
// The states are keyed by node tag and stored as one whole: a write replaces everything the
// previous write left, so a node that is gone simply stops being written and its record is
// carried until it expires.
type HealthStateStore interface {
	// LoadHealthStates reads the states stored for the given fingerprint, which describes
	// the measurement they belong to. A state recorded under a different fingerprint is not
	// returned: the delays it holds were measured on another path.
	LoadHealthStates(fingerprint []byte) map[string]HealthState
	// SaveHealthStates replaces the stored states, which stay usable until expiresAt: the
	// store is one value, so the whole set expires together and a reader drops it as a whole.
	// A nil or empty map clears them.
	SaveHealthStates(fingerprint []byte, states map[string]HealthState, expiresAt time.Time) error
}
