package agent

import (
	"context"
	"log"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
)

type queryFn = func(context.Context, *client.Client) (*types.Container, error)

type findContainerQuery struct {
	fn   queryFn
	name string
}

func findContainer(ctx context.Context, dockerCli *client.Client) (string, error) {
	queries := []findContainerQuery{
		{findByLabel, "findByLabel"},
		{findByImage, "findByImage"},
		{findByLogs, "findByLogs"},
	}

	for _, query := range queries {
		container, err := query.fn(ctx, dockerCli)
		if err != nil {
			return "", errors.WithMessagef(err, "failed finding container %s", query.name)
		}

		if container != nil {
			log.Printf("Found container %s: %s", query.name, container.ID)
			return container.ID, nil
		}
	}

	return "", errors.New("unable to find container")
}

func findByLabel(ctx context.Context, dockerCli *client.Client) (*types.Container, error) {
	filters := filters.NewArgs()
	filters.Add("status", "running")
	filters.Add("label", "io.portainer.agent=true")

	containers, err := dockerCli.ContainerList(ctx, types.ContainerListOptions{
		Filters: filters,
	})
	if err != nil {
		return nil, errors.WithMessage(err, "unable to list containers")
	}

	if len(containers) == 0 {
		return nil, nil
	}

	if len(containers) > 1 {
		return nil, errors.New("multiple containers found")
	}

	return &containers[0], nil
}

func findByImage(ctx context.Context, dockerCli *client.Client) (*types.Container, error) {
	filters := filters.NewArgs()
	filters.Add("status", "running")

	// not using the ancestor filter because it looks for latest tag

	containers, err := dockerCli.ContainerList(ctx, types.ContainerListOptions{
		Filters: filters,
	})
	if err != nil {
		return nil, errors.WithMessage(err, "unable to list containers")
	}

	for _, container := range containers {
		if strings.HasPrefix(container.Image, "portainer/agent") || strings.HasPrefix(container.Image, "portainerci/agent") {
			return &container, nil
		}
	}

	return nil, nil
}

func findByLogs(ctx context.Context, dockerCli *client.Client) (*types.Container, error) {
	filters := filters.NewArgs()
	filters.Add("status", "running")

	containers, err := dockerCli.ContainerList(ctx, types.ContainerListOptions{
		Filters: filters,
	})
	if err != nil {
		return nil, errors.WithMessage(err, "unable to list containers")
	}

	for _, container := range containers {
		logs, err := dockerCli.ContainerLogs(ctx, container.ID, types.ContainerLogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Timestamps: true,
		})
		if err != nil {
			return nil, errors.WithMessage(err, "unable to get container logs")
		}

		buf := make([]byte, 1024)
		for {
			n, err := logs.Read(buf)
			if err != nil {
				break
			}

			if strings.Contains(string(buf[:n]), "Starting Agent API server") {
				return &container, nil
			}
		}
	}

	return nil, nil
}
