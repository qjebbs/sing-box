package cachefile

import (
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/sagernet/bbolt"
	"github.com/sagernet/sing-box/adapter"
)

var bucketHealthState = []byte("health_state")

// healthStateRecord is what one node keeps. It is JSON rather than the packed binary the other
// buckets use because it has named fields rather than one blob: a field can be added or dropped
// without a codec, a version, or a reader that knows the layout. It is a few hundred bytes
// written a handful of times an hour, so nothing is bought by packing it: the day of slots is
// 48 numbers of a handful of digits, and it needs no timestamp of its own.
type healthStateRecord struct {
	// ExpiresAt is when this record stops being worth reading: the life of the longest-lived
	// part, see healthcheck.healthStateTimeout.
	ExpiresAt time.Time `json:"expires_at"`
	adapter.HealthState
}

// healthStatesOf returns the bucket holding the records of one measurement. The key is the
// path the node is measured through — the target and the relays — not the check that measures
// it: a record describes the node as that path reaches it, so two checks on the same path share
// the records of the nodes they share, and two checks on different paths never do.
//
// Sharing is exact enough because what is stored is a property of the path: on a day of real
// history every node came out with the same floor whether it was sampled two times a window or
// a hundred, so the two views of a shared record differ by noise rather than by provenance.
func healthStatesOf(tx *bbolt.Tx, c *CacheFile, fingerprint []byte) *bbolt.Bucket {
	bucket := c.bucket(tx, bucketHealthState)
	if bucket == nil {
		return nil
	}
	return bucket.Bucket([]byte(hex.EncodeToString(fingerprint)))
}

// LoadHealthStates implements adapter.HealthStateStore.
func (c *CacheFile) LoadHealthStates(fingerprint []byte) map[string]adapter.HealthState {
	var states map[string]adapter.HealthState
	err := c.view(func(tx *bbolt.Tx) error {
		bucket := healthStatesOf(tx, c, fingerprint)
		if bucket == nil {
			return nil
		}
		states = make(map[string]adapter.HealthState)
		return bucket.ForEach(func(tag, content []byte) error {
			state, ok := decodeHealthState(content)
			if !ok {
				return nil
			}
			states[string(tag)] = state
			return nil
		})
	})
	if err != nil {
		return nil
	}
	return states
}

// SaveHealthStates implements adapter.HealthStateStore.
//
// Each node is written under its own key and nothing else is touched: a check writes only the
// nodes it measures, so the records of a check that shares the file are never rewritten from a
// copy that may already be out of date. A record of a node that is gone is left to expire,
// which is what the open of the file removes.
func (c *CacheFile) SaveHealthStates(fingerprint []byte, states map[string]adapter.HealthState, expiresAt time.Time) error {
	if len(states) == 0 {
		return nil
	}
	encoded := make(map[string][]byte, len(states))
	for tag, state := range states {
		content, err := encodeHealthState(state, expiresAt)
		if err != nil {
			return err
		}
		encoded[tag] = content
	}
	key := []byte(hex.EncodeToString(fingerprint))
	return c.batch(func(tx *bbolt.Tx) error {
		bucket, err := c.createBucket(tx, bucketHealthState)
		if err != nil {
			return err
		}
		bucket, err = bucket.CreateBucketIfNotExists(key)
		if err != nil {
			return err
		}
		for tag, content := range encoded {
			err = bucket.Put([]byte(tag), content)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// dropExpiredHealthStates removes the records that have outlived their validity. It is what lets
// a record outlive the check that wrote it: a configuration that no longer has a health check
// never writes again, so nothing else would ever remove it.
//
// It runs when the file is opened, which is the one moment every configuration passes through.
// There is no periodic scan like the DNS and RDRC caches have: those hold one entry per query
// and must reclaim them as they go, while this holds one per node of a configured check, and a
// check that is gone stays gone.
func (c *CacheFile) dropExpiredHealthStates(db *bbolt.DB) error {
	return db.Batch(func(tx *bbolt.Tx) error {
		bucket := c.bucket(tx, bucketHealthState)
		if bucket == nil {
			return nil
		}
		var emptyChecks [][]byte
		err := bucket.ForEachBucket(func(key []byte) error {
			check := bucket.Bucket(key)
			if check == nil {
				return nil
			}
			var expired [][]byte
			err := check.ForEach(func(tag, content []byte) error {
				if _, ok := decodeHealthState(content); !ok {
					expired = append(expired, append([]byte(nil), tag...))
				}
				return nil
			})
			if err != nil {
				return err
			}
			for _, tag := range expired {
				err = check.Delete(tag)
				if err != nil {
					return err
				}
			}
			if check.Stats().KeyN <= len(expired) {
				emptyChecks = append(emptyChecks, append([]byte(nil), key...))
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, key := range emptyChecks {
			err = bucket.DeleteBucket(key)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func encodeHealthState(state adapter.HealthState, expiresAt time.Time) ([]byte, error) {
	return json.Marshal(healthStateRecord{
		ExpiresAt:   expiresAt,
		HealthState: state,
	})
}

// decodeHealthState reads one record, reporting false for one that cannot be read or has
// expired: a record this build cannot use is one the next start would not use either.
func decodeHealthState(content []byte) (adapter.HealthState, bool) {
	var record healthStateRecord
	if json.Unmarshal(content, &record) != nil {
		return adapter.HealthState{}, false
	}
	if !record.ExpiresAt.IsZero() && time.Now().After(record.ExpiresAt) {
		return adapter.HealthState{}, false
	}
	return record.HealthState, true
}
