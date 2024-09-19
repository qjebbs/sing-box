package healthcheck

import (
	"sync"
)

// MetaData is the context for health check, it collects network connectivity
// status and checked status of outbounds
//
// About connectivity status collection:
//
// Consider the health checks are done asynchronously, success checks will
// report network is available in a short time, after that, there will be
// failure checks query the network connectivity. So,
//
// 1. In cases of any one check success, the network is known and reported
// to be available.
//
// 2. In cases of all checks failed, we can not distinguesh from the network is
// down or all nodes are dead. But the health check is aimed to tell which nodes
// are better, a all-failed result doesn't contribute to the objective, so we just
// assume the network is down and ignore the all-failed result.
type MetaData struct {
	sync.Mutex

	anySuccess bool
	checked    map[string]bool
}

// NewMetaData creates a new MetaData
func NewMetaData() *MetaData {
	return &MetaData{
		checked: make(map[string]bool),
	}
}

// ReportChecked reports the outbound of the tag is checked
func (c *MetaData) ReportChecked(tag string) {
	c.Lock()
	defer c.Unlock()
	c.checked[tag] = true
}

// Checked tells if the outbound of the tag is checked
func (c *MetaData) Checked(tag string) bool {
	c.Lock()
	defer c.Unlock()
	return c.checked[tag]
}

// ReportSuccess reports a check success, which means the network is OK
func (c *MetaData) ReportSuccess() {
	c.Lock()
	defer c.Unlock()
	c.anySuccess = true
}

// AnySuccess tells if there is any check success.
// If false, all nodes are down, or the network is unavailable.
func (c *MetaData) AnySuccess() bool {
	c.Lock()
	defer c.Unlock()
	return c.anySuccess
}
