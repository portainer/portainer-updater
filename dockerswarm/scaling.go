package dockerswarm

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"

	"github.com/portainer/portainer-updater/utils"
)

func Scale(ctx context.Context, dockerClient client.APIClient, serviceID string, replicas uint64, timeout time.Duration) error {
	service, _, err := dockerClient.ServiceInspectWithRaw(ctx, serviceID, types.ServiceInspectOptions{})
	if err != nil {
		return fmt.Errorf("could not inspect service: %w", err)
	}

	if service.Spec.Mode.Replicated == nil || service.Spec.Mode.Replicated.Replicas == nil {
		return fmt.Errorf("service is not in replicated mode")
	}

	if *service.Spec.Mode.Replicated.Replicas == replicas {
		return nil
	}

	prevVersion := service.Version
	service.Version = swarm.Version{Index: service.Version.Index + 1}

	service.Spec.Mode.Replicated.Replicas = &replicas

	if _, err := dockerClient.ServiceUpdate(ctx, service.ID, prevVersion, service.Spec, types.ServiceUpdateOptions{}); err != nil {
		return fmt.Errorf("could not update service: %w", err)
	}

	// When scaling replicas, the service update is considered complete when the desired number of tasks are running.
	// There is no UpdateStatus like there is when performing a rolling update.
	err = utils.WaitUntil(ctx, func() (bool, error) {
		tasks, err := dockerClient.TaskList(ctx, types.TaskListOptions{
			Filters: filters.NewArgs(
				filters.Arg("service", service.ID),
			),
		})
		if err != nil {
			return false, err
		}

		running := 0
		for _, task := range tasks {
			if task.Status.State == swarm.TaskStateRunning {
				running++
			}
		}

		return uint64(running) == replicas, nil
	}, timeout, time.Second)
	if err != nil {
		return fmt.Errorf("could not wait for service update to complete: %w", err)
	}

	return nil
}
