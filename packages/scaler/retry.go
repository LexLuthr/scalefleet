package scaler

import (
	"context"
	"time"

	"golang.org/x/xerrors"
)

func callWithRetry[T any](ctx context.Context, cfg *Config, operation string, attempts int, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	if attempts < 1 {
		attempts = 1
	}

	for attempt := 1; attempt <= attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, err
		}

		callCtx, cancel := context.WithTimeout(ctx, cfg.Runtime.APICallTimeout)
		result, err := fn(callCtx)
		cancel()
		if err == nil {
			return result, nil
		}

		if attempt == attempts {
			return zero, xerrors.Errorf("%s failed after %d attempt(s): %w", operation, attempt, err)
		}

		backoff := cfg.Runtime.APIInitialBackoff * time.Duration(1<<(attempt-1))
		log.Warnw("API call failed, retrying",
			"operation", operation,
			"attempt", attempt,
			"error", err.Error(),
			"backoff", backoff.String())

		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(backoff):
		}
	}

	return zero, xerrors.Errorf("%s failed: exhausted attempts", operation)
}
