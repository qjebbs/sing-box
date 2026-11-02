package base

import (
	"context"

	"github.com/sagernet/sing-box/log"
	E "github.com/sagernet/sing/common/exceptions"
)

// maxAttempts is the number of nodes a connection tries before it gives up.
const maxAttempts = 5

// tryEach picks a node and calls connect with it until a call succeeds. A node that
// fails is reported, so the group checks it again and the next pick avoids it: a
// connection never retries the node that just failed.
func tryEach[N any, T any](
	ctx context.Context,
	logger log.ContextLogger,
	pick func() (N, bool),
	failed func(node N),
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
		failed(node)
		lastErr = err
		logger.ErrorContext(ctx, err)
	}
	return value, lastErr
}
