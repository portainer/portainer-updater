package utils

import (
	"context"
	"errors"
	"time"
)

func WaitUntil(ctx context.Context, condition func() bool, timeout, timeBetweenTries time.Duration) error {
	for timeoutCh := time.After(timeout); ; {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeoutCh:
			return errors.New("timeout")
		default:
			if condition() {
				return nil
			}

			time.Sleep(timeBetweenTries)
		}
	}
}
