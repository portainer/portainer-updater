package dockerswarm

import (
	"context"

	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/portainer/portainer-updater/dockerstandalone"
)

func FindAgentService(ctx context.Context, dockerCli *client.Client) (*swarm.Service, error) {
	container, err := dockerstandalone.FindAgentContainer(ctx, dockerCli)
	if err != nil {
		return nil, err
	}

	return findService(ctx, container, dockerCli)
}
