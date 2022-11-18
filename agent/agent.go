package agent

import (
	"context"
	"os"
	"path"
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

	return nomad.Update(ctx, nomadCli, job, task, r.Image, r.ScheduleId)
}

// WriteFile takes a path, filename, a file and the mode that should be associated
// to the file and writes it to disk
func WriteFile(folder, filename string, file []byte, mode uint32) error {
	err := os.MkdirAll(folder, 0755)
	if err != nil {
		return err
	}

	filePath := path.Join(folder, filename)

	return os.WriteFile(filePath, file, os.FileMode(mode))
}
