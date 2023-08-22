package portainer

import (
	"context"
	"fmt"
	"strings"

	"github.com/Masterminds/semver"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/portainer/portainer-updater/dockerstandalone"
	"github.com/portainer/portainer-updater/dockerswarm"
	"github.com/portainer/portainer-updater/kubernetes"
	"github.com/rs/zerolog/log"
)

type EnvType string

const (
	EnvTypeDockerStandalone EnvType = "standalone"
	EnvTypeKubernetes       EnvType = "kubernetes"
	EnvTypeSwarm            EnvType = "swarm"
)

type Command struct {
	EnvType EnvType `help:"The environment type" default:"standalone" enum:"standalone,swarm,kubernetes"`
	License string  `help:"License key to use for Portainer EE"`
	Image   string  `help:"Image of portainer to upgrade to. e.g. portainer/portainer-ee:latest" name:"image" default:"portainer/portainer-ee:latest"`
}

func (r *Command) Run() error {
	ctx := context.Background()

	r.validateImageWithLicense()

	switch r.EnvType {
	case EnvTypeDockerStandalone:
		return r.runStandalone(ctx)
	case EnvTypeSwarm:
		return r.runSwarm(ctx)
	case EnvTypeKubernetes:
		return r.runKubernetes(ctx)
	}

	return errors.Errorf("unknown environment type: %s", r.EnvType)
}

func (r *Command) validateImageWithLicense() {
	if !strings.HasPrefix(r.License, "3-") {
		log.Debug().
			Str("license", r.License).
			Msg("License is a valid type 2 Portainer EE license, leaving it as is")
		return
	}

	parts := strings.Split(r.Image, ":")
	if len(parts) != 2 {
		log.Debug().
			Str("imageName", r.Image).
			Msg("Image name is not a standard image (image:tag), leaving it as is")
		return
	}

	imageName := parts[0]
	tag := parts[1]

	if imageName != "portainer-ee" {
		log.Debug().
			Str("imageName", r.Image).
			Msg("Image name is not portainer-ee, leaving it as is")
		return
	}

	requiredVersion, err := semver.NewVersion(tag)
	if err != nil {
		log.Debug().
			Err(err).
			Str("tag", tag).
			Msg("Tag is not a valid semver, leaving it as is")
		return
	}

	minVersion := "2.18.4"
	if requiredVersion.GreaterThan(semver.MustParse(minVersion)) {
		log.Debug().
			Str("tag", tag).
			Str("minVersion", minVersion).
			Msg("Tag is higher than minimum version, leaving it as is")
		return
	}

	r.Image = fmt.Sprintf("%s:%s", imageName, minVersion)
}

func (r *Command) runKubernetes(ctx context.Context) error {
	cli, err := kubernetes.GetClient()
	if err != nil {
		return errors.WithMessage(err, "failed getting kubernetes client")
	}

	log.Info().
		Str("image", r.Image).
		Msg("Updating Portainer on kubernetes environment")

	deployment, err := kubernetes.FindPortainerDeployment(ctx, cli)
	if err != nil {
		return errors.WithMessage(err, "failed finding deployment")
	}

	log.Debug().
		Str("deployment", deployment.Name).
		Msg("Found deployment")

	return kubernetes.Update(ctx, cli, r.Image, deployment, r.License)

}

func (r *Command) runStandalone(ctx context.Context) error {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatal().Err(err).Msg("Unable to initialize Docker client")
	}

	log.Info().
		Str("image", r.Image).
		Msg("Updating Portainer on standalone environment")

	oldContainer, err := dockerstandalone.FindPortainerContainer(ctx, dockerCli)
	if err != nil {
		return errors.WithMessage(err, "failed finding container")
	}

	return dockerstandalone.Update(ctx, dockerCli, oldContainer.ID, r.Image, func(config *container.Config) {
		if r.License != "" {
			config.Env = append(config.Env, "PORTAINER_LICENSE_KEY="+r.License)
		}
	})
}

func (r *Command) runSwarm(ctx context.Context) error {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatal().Err(err).Msg("Unable to initialize Docker client")
	}

	log.Info().
		Str("image", r.Image).
		Msg("Updating Portainer on swarm environment")

	service, err := dockerswarm.FindPortainerService(ctx, dockerCli)
	if err != nil {
		return errors.WithMessage(err, "failed finding container id")
	}

	return dockerswarm.Update(ctx, dockerCli, r.Image, service, func(config *swarm.ContainerSpec) {
		if r.License != "" {
			config.Env = append(config.Env, "PORTAINER_LICENSE_KEY="+r.License)
		}
	})
}
