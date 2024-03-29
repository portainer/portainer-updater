package agent

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/hashicorp/nomad/api"
	"github.com/pkg/errors"
	"github.com/portainer/portainer-updater/dockerstandalone"
	"github.com/portainer/portainer-updater/nomad"
	"github.com/rs/zerolog/log"
)

// UpdateScheduleIDLabel is the label used to store the update schedule ID
const UpdateScheduleIDLabel = "io.portainer.update.scheduleId"

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
	ctx := context.Background()

	switch r.EnvType {
	case "standalone":
		return r.runStandalone(ctx)
	case "nomad":
		return r.runNomad(ctx)
	}

	return errors.Errorf("unknown environment type: %s", r.EnvType)
}

func (r *AgentCommand) runStandalone(ctx context.Context) error {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatal().Err(err).Msg("Unable to initialize Docker client")
	}

	log.Info().
		Str("image", r.Image).
		Str("schedule-id", r.ScheduleId).
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

func (r *AgentCommand) runNomad(ctx context.Context) error {
	nomadConfig := api.DefaultConfig()

	nomadAddress := os.Getenv(nomad.NomadAddrEnvVarName)
	if strings.HasPrefix(nomadAddress, "https") {
		tls := &api.TLSConfig{
			CACertPEM:     []byte(os.Getenv(nomad.NomadCACertContentEnvVarName)),
			ClientCertPEM: []byte(os.Getenv(nomad.NomadClientCertContentEnvVarName)),
			ClientKeyPEM:  []byte(os.Getenv(nomad.NomadClientKeyContentEnvVarName)),
		}
		nomadConfig.TLSConfig = tls
	}

	nomadCli, err := api.NewClient(nomadConfig)
	if err != nil {
		return errors.WithMessage(err, "failed to initialize Nomad client")
	}

	job, task, err := nomad.FindAgentContainer(ctx, nomadCli)
	if err != nil {
		return errors.WithMessage(err, "failed finding container id")
	}

	if task.Env == nil {
		task.Env = make(map[string]string, 0)
	}

	// add nomad env
	task.Env[nomad.NomadAddrEnvVarName] = os.Getenv(nomad.NomadAddrEnvVarName)
	task.Env[nomad.NomadNamespaceEnvVarName] = os.Getenv(nomad.NomadNamespaceEnvVarName)
	task.Env[nomad.NomadRegionEnvVarName] = os.Getenv(nomad.NomadRegionEnvVarName)
	task.Env[nomad.NomadTokenEnvVarName] = os.Getenv(nomad.NomadTokenEnvVarName)
	// add nomad tls certificate info env
	task.Env[nomad.NomadCACertContentEnvVarName] = os.Getenv(nomad.NomadCACertContentEnvVarName)
	task.Env[nomad.NomadClientCertContentEnvVarName] = os.Getenv(nomad.NomadClientCertContentEnvVarName)
	task.Env[nomad.NomadClientKeyContentEnvVarName] = os.Getenv(nomad.NomadClientKeyContentEnvVarName)
	// add portainer agent env
	task.Env[nomad.EnvKeyEdge] = os.Getenv(nomad.EnvKeyEdge)
	task.Env[nomad.EnvKeyEdgeKey] = os.Getenv(nomad.EnvKeyEdgeKey)
	task.Env[nomad.EnvKeyEdgeID] = os.Getenv(nomad.EnvKeyEdgeID)
	task.Env[nomad.EnvKeyEdgeInsecurePoll] = os.Getenv(nomad.EnvKeyEdgeInsecurePoll)
	task.Env[nomad.EnvKeyAgentSecret] = os.Getenv(nomad.EnvKeyAgentSecret)
	// add update id
	task.Env[nomad.EnvKeyUpdateID] = r.ScheduleId

	return nomad.Update(ctx, nomadCli, job, task, r.Image, r.ScheduleId)
}
