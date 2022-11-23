package dockerswarm

import (
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/portainer/portainer-updater/dockerstandalone"
)

func FindPortainerService(ctx context.Context, dockerCli *client.Client) (*swarm.Service, error) {
	container, err := dockerstandalone.FindPortainerContainer(ctx, dockerCli)
	if err != nil {
		return nil, err
	}

	serviceName := container.Labels["com.docker.swarm.service.name"]
	if serviceName == "" {
		return nil, errors.New("unable to find service name")
	}

	serviceFilters := filters.NewArgs()
	serviceFilters.Add("name", serviceName)
	services, err := dockerCli.ServiceList(ctx, types.ServiceListOptions{
		Filters: serviceFilters,
	})

	if err != nil {
		return nil, errors.WithMessage(err, "unable to list services")
	}

	if len(services) == 0 {
		return nil, nil
	}

	if len(services) > 1 {
		return nil, errors.New("multiple services found")
	}

	return &services[0], nil

}
