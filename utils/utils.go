package utils

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func WaitUntil(ctx context.Context, condition func() (bool, error), timeout, timeBetweenTries time.Duration) error {
	for timeoutCh := time.After(timeout); ; {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeoutCh:
			return errors.New("timeout")
		default:
			ok, err := condition()
			if err != nil {
				return fmt.Errorf("wait until error: %w", err)
			}
			if ok {
				return nil
			}

			time.Sleep(timeBetweenTries)
		}
	}
}
