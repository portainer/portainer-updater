package dockerstandalone

import (
	"bufio"
	"context"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
)

func findByLabelFn(label string) queryFn {
	return func(ctx context.Context, dockerCli *client.Client) (*types.Container, error) {
		filters := filters.NewArgs()
		filters.Add("status", "running")
		filters.Add("label", label)

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
}

func findByImageFn(possibleImagePrefixes ...string) queryFn {
	return func(ctx context.Context, dockerCli *client.Client) (*types.Container, error) {
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
			for _, possibleImage := range possibleImagePrefixes {
				if container.Image == possibleImage {
					return &container, nil
				}
			}
		}

		return nil, nil
	}
}

func findByLogsFn(log string) queryFn {

	return func(ctx context.Context, dockerCli *client.Client) (*types.Container, error) {
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
			})
			if err != nil {
				return nil, errors.WithMessage(err, "unable to get container logs")
			}

			scanner := bufio.NewScanner(logs)

			for scanner.Scan() {
				if strings.Contains(scanner.Text(), log) {
					return &container, nil
				}
			}

			if err := scanner.Err(); err != nil {
				return nil, errors.WithMessage(err, "unable to read container logs")
			}
		}

		return nil, nil
	}
}
