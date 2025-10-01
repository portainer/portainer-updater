package dockerstandalone

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

func RollbackDB(ctx context.Context, dockerClient client.APIClient, rollbackContainerID string, timeout time.Duration) error {
	if err := dockerClient.ContainerStart(ctx, rollbackContainerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("unable to start new container: %w", err)
	}

	waitResponseCh, errCh := dockerClient.ContainerWait(ctx, rollbackContainerID, container.WaitConditionNotRunning)
	var rollbackErr error
	select {
	case err := <-errCh:
		if err != nil {
			rollbackErr = fmt.Errorf("waiting for container failed: %w", err)
		}
	case waitResponse := <-waitResponseCh:
		if waitResponse.StatusCode != 0 {
			err := fmt.Errorf("exit code %d", waitResponse.StatusCode)
			if waitResponse.Error != nil {
				err = fmt.Errorf("%w: %s", err, waitResponse.Error.Message)
			}

			rollbackErr = err
		}
	case <-time.After(timeout):
		rollbackErr = errors.New("timeout waiting for rollback container to finish")
	}

	// Clean up the rollback container even if the rollback itself failed
	if err := dockerClient.ContainerRemove(ctx, rollbackContainerID, container.RemoveOptions{Force: true}); err != nil {
		rollbackErr = errors.Join(rollbackErr, fmt.Errorf("unable to remove rollback container %s: %w", rollbackContainerID, err))
	}

	if rollbackErr != nil {
		return rollbackErr
	}

	return nil
}
