package agent

import (
	"context"

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

type AgentCommand struct {
	EnvType    EnvType `kong:"help='The environment type',default='standalone',enum='standalone,nomad'"`
	ScheduleId string  `arg:"" help:"Schedule ID of the agent to upgrade to. e.g. 1" name:"schedule-id"`
	Image      string  `arg:"" help:"Image of the agent to upgrade to. e.g. portainer/agent:latest" name:"image" default:"portainer/agent:latest"`
}

func (r *AgentCommand) Run() error {
	switch r.EnvType {
	case "standalone":
		return r.runStandalone()
	case "nomad":
		// return r.runNomad()
	}

	return errors.Errorf("unknown environment type: %s", r.EnvType)
}

func (r *AgentCommand) runStandalone() error {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatal().Err(err).Msg("Unable to initialize Docker client")
	}

	ctx := context.Background()

	log.Info().
		Str("image", r.Image).
		Str("schedule-id", r.ScheduleId).
		Msg("Updating Portainer agent")
	oldContainer, err := dockerstandalone.FindAgentContainer(ctx, dockerCli)
	if err != nil {
		return errors.WithMessage(err, "failed finding container id")
	}

	if oldContainer.Labels != nil && oldContainer.Labels[dockerstandalone.UpdateScheduleIDLabel] == r.ScheduleId {
		log.Info().Msg("Agent already updated")

		return nil
	}

	return dockerstandalone.Update(ctx, dockerCli, oldContainer.ID, r.Image, r.ScheduleId)
}
