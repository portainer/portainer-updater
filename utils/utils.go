package utils

import (
	"context"
	"errors"
	"time"
)

func WaitUntil(ctx context.Context, condition func() bool, timeout, timeBetweenTries time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(timeout):
		return errors.New("timeout")
	default:
		if condition() {
			return nil
		}

		time.Sleep(timeBetweenTries)
	}

	return WaitUntil(ctx, condition, timeout, timeBetweenTries)
}
