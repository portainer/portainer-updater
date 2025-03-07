package agent

import (
	"context"
	"fmt"
	"github.com/docker/docker/api/types/swarm"
	"github.com/portainer/portainer-updater/dockerswarm"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/portainer/portainer-updater/dockerstandalone"
	"github.com/rs/zerolog/log"
)

// UpdateScheduleIDLabel is the label used to store the update schedule ID
const UpdateScheduleIDLabel = "io.portainer.update.scheduleId"

type EnvType string

const (
	EnvTypeDocker EnvType = "docker"
	// deprecated
	EnvTypeDockerStandalone EnvType = "standalone"
)

type AgentCommand struct {
	EnvType    EnvType `help:"The environment type. Supported types: 'docker' 'standalone'(deprecated)" default:"docker" enum:"docker,standalone"`
	ScheduleId string  `arg:"" help:"Schedule ID of the agent to upgrade to. e.g. 1" name:"schedule-id"`
	Image      string  `arg:"" help:"Image of the agent to upgrade to. e.g. portainer/agent:latest" name:"image" default:"portainer/agent:latest"`
}

func (r *AgentCommand) Run() error {
	ctx := context.Background()

	switch r.EnvType {
	case EnvTypeDocker, EnvTypeDockerStandalone:
		return r.runDocker(ctx)
	}

	return errors.Errorf("unknown environment type: %s", r.EnvType)
}

func (r *AgentCommand) runDocker(ctx context.Context) error {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}
	defer dockerCli.Close()

	dockerInfo, err := dockerCli.Info(context.Background())
	if err != nil {
		return errors.Wrap(err, "unable to get docker info")
	}

	if dockerInfo.Swarm.NodeID != "" {
		return r.runSwarm(ctx, dockerCli)
	}

	return r.runStandalone(ctx, dockerCli)
}

func (r *AgentCommand) runSwarm(ctx context.Context, dockerCli *client.Client) error {
	log.Info().
		Str("image", r.Image).
		Str("schedule-id", r.ScheduleId).
		Str("env", "swarm").
		Msg("Updating Portainer agent")
	service, err := dockerswarm.FindAgentService(ctx, dockerCli)
	if err != nil {
		return errors.WithMessage(err, "failed finding service")
	}

	return dockerswarm.Update(ctx, dockerCli, r.Image, service, func(config *swarm.ContainerSpec) {
		if config.Labels == nil {
			config.Labels = make(map[string]string)
		}

		config.Labels[UpdateScheduleIDLabel] = r.ScheduleId
	})
}

func (r *AgentCommand) runStandalone(ctx context.Context, dockerCli *client.Client) error {
	log.Info().
		Str("image", r.Image).
		Str("schedule-id", r.ScheduleId).
		Str("env", "standalone").
		Msg("Updating Portainer agent")
	oldContainer, err := dockerstandalone.FindAgentContainer(ctx, dockerCli)
	if err != nil {
		return errors.WithMessage(err, "failed finding container id")
	}

	if oldContainer.Labels != nil && oldContainer.Labels[UpdateScheduleIDLabel] == r.ScheduleId {
		log.Info().Msg("Agent already updated")

		return nil
	}

	return dockerstandalone.Update(ctx, dockerCli, oldContainer.ID, r.Image, func(config *container.Config) {
		foundIndex := -1
		for index, env := range config.Env {
			if strings.HasPrefix(env, "UPDATE_ID=") {
				foundIndex = index
			}
		}

		scheduleEnv := fmt.Sprintf("UPDATE_ID=%s", r.ScheduleId)
		if foundIndex != -1 {
			config.Env[foundIndex] = scheduleEnv
		} else {
			config.Env = append(config.Env, scheduleEnv)
		}

		if config.Labels == nil {
			config.Labels = make(map[string]string)
		}

		config.Labels[UpdateScheduleIDLabel] = r.ScheduleId
	})
}
