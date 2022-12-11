package dockerstandalone

import (
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

func FindPortainerContainer(ctx context.Context, dockerCli *client.Client) (*types.Container, error) {
	queries := []findContainerQuery{
		{findByLabelFn("io.portainer.server=true"), "findByLabel"},
		{findByImageFn("portainer/portainer", "portainerci/portainer"), "findByImage"},
		{findByLogsFn("starting Portainer"), "findByLogs"},
	}

	for _, query := range queries {
		container, err := query.fn(ctx, dockerCli)
		if err != nil {
			return nil, errors.WithMessagef(err, "failed finding container %s", query.name)
		}

		if container != nil {
			log.Debug().
				Str("container", container.ID).
				Str("query", query.name).
				Msg("Found container")
			return container, nil
		}
	}

	return nil, errors.New("unable to find container")
}
