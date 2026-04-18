package dockerswarm

import (
	"context"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

func findService(ctx context.Context, ctr *container.Summary, dockerCli *client.Client) (*swarm.Service, error) {
	serviceID := ctr.Labels["com.docker.swarm.service.id"]
	if serviceID == "" {
		log.Debug().
			Str("container", ctr.ID).
			Interface("labels", ctr.Labels).
			Msg("Container is not part of a service")

		return nil, errors.New("unable to find service name")
	}

	serviceFilters := filters.NewArgs()
	serviceFilters.Add("id", serviceID)
	services, err := dockerCli.ServiceList(ctx, swarm.ServiceListOptions{
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
