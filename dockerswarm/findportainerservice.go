package dockerswarm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
)

func FindPortainerService(ctx context.Context, dockerCli client.APIClient) (*swarm.Service, error) {
	serviceFilters := filters.NewArgs()
	serviceFilters.Add("name", "portainer")  // Assuming the service is named "portainer" in swarm
	serviceFilters.Add("mode", "replicated") // Portainer should always be deployed as a replicated service
	services, err := dockerCli.ServiceList(ctx, types.ServiceListOptions{
		Filters: serviceFilters,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list portainer services: %w", err)
	}

	if len(services) == 0 {
		return nil, errors.New("no services found")
	}

	for _, service := range services {
		// Skip services that are marked as updaters
		// this is to avoid updating the updater itself
		if _, ok := service.Spec.Labels["io.portainer.updater"]; ok {
			continue
		}
		if _, ok := service.Spec.TaskTemplate.ContainerSpec.Labels["io.portainer.updater"]; ok {
			continue
		}

		for _, c := range service.Spec.TaskTemplate.Placement.Constraints {
			if strings.ReplaceAll(c, " ", "") == "node.role==manager" {
				return &service, nil
			}
		}
	}

	return nil, errors.New("no portainer service found")
}
