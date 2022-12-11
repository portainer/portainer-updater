package dockerstandalone

import (
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

type queryFn = func(context.Context, *client.Client) (*types.Container, error)

type findContainerQuery struct {
	fn   queryFn
	name string
}

func FindAgentContainer(ctx context.Context, dockerCli *client.Client) (*types.Container, error) {
	queries := []findContainerQuery{
		{findByLabelFn("io.portainer.agent=true"), "findByLabel"},
		{findByImageFn("portainer/agent", "portainer/agent"), "findByImage"},
		{findByLogsFn("Starting Agent API server"), "findByLogs"},
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
