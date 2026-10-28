package group

import (
	"context"

	"github.com/sagernet/sing-box/log"
	E "github.com/sagernet/sing/common/exceptions"
)

// maxAttempts is the number of nodes a connection tries before it gives up.
const maxAttempts = 5

// tryEach picks a node and calls connect with it until a call succeeds. A failed
// node is reported through report, so the next pick avoids it: a connection never
// retries the node that just failed.
func tryEach[N any, T any](
	ctx context.Context,
	logger log.ContextLogger,
	pick func() (N, bool),
	report func(N),
	connect func(N) (T, error),
) (T, error) {
	var (
		value   T
		lastErr error
	)
	for i := 0; i < maxAttempts; i++ {
		node, loaded := pick()
		if !loaded {
			lastErr = E.New("no outbound available")
			break
		}
		attempt, err := connect(node)
		if err == nil {
			return attempt, nil
		}
		lastErr = err
		logger.ErrorContext(ctx, err)
		report(node)
	}
	return value, lastErr
}
