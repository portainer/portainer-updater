package portainer

import (
	"context"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/portainer/portainer-updater/dockerstandalone"
	"github.com/rs/zerolog/log"
)

type EnvType string

const (
	EnvTypeDockerStandalone EnvType = "standalone"
	EnvTypeNomad            EnvType = "nomad"
)

type Command struct {
	EnvType EnvType `help:"The environment type" default:"standalone" enum:"standalone,nomad"`
	License string  `help:"License key to use for Portainer EE"`
	Image   string  `help:"Image of portainer to upgrade to. e.g. portainer/portainer-ee:latest" name:"image" default:"portainer/portainer-ee:latest"`
}

func (r *Command) Run() error {
	ctx := context.Background()

	switch r.EnvType {
	case "standalone":
		return r.runStandalone(ctx)
	}

	return errors.Errorf("unknown environment type: %s", r.EnvType)
}

func (r *Command) runStandalone(ctx context.Context) error {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatal().Err(err).Msg("Unable to initialize Docker client")
	}

	log.Info().
		Str("image", r.Image).
		Msg("Updating Portainer agent")

	oldContainer, err := dockerstandalone.FindPortainerContainer(ctx, dockerCli)
	if err != nil {
		return errors.WithMessage(err, "failed finding container id")
	}

	return dockerstandalone.Update(ctx, dockerCli, oldContainer.ID, r.Image, func(config *container.Config) {
		if r.License != "" {
			config.Env = append(config.Env, "PORTAINER_LICENSE_KEY="+r.License)
		}
	})
}
