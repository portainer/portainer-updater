package agent

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/docker/docker/client"
	"github.com/hashicorp/nomad/api"
	"github.com/pkg/errors"
	"github.com/portainer/portainer-updater/dockerstandalone"
	"github.com/portainer/portainer-updater/nomad"
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

	if oldContainer.Labels != nil && oldContainer.Labels[dockerstandalone.UpdateScheduleIDLabel] == r.ScheduleId {
		log.Info().Msg("Agent already updated")

		return nil
	}

	return dockerstandalone.Update(ctx, dockerCli, oldContainer.ID, r.Image, r.ScheduleId)
}

func (r *AgentCommand) runNomad(ctx context.Context) error {

	envs := parseEnvs()
	nomadAddress := envs[nomad.NomadAddrEnvVarName]
	if strings.HasPrefix(nomadAddress, "https") {
		// export the https address
		os.Setenv(nomad.NomadAddrEnvVarName, nomadAddress)

		// export the namespace
		os.Setenv(nomad.NomadNamespaceEnvVarName, envs[nomad.NomadNamespaceEnvVarName])

		// Write the TLS certificate into files and export the path as the environment variables
		nomadCACertContent := envs[nomad.NomadCACertContentEnvVarName]
		if len(nomadCACertContent) == 0 {
			log.Fatal().Msg("nomad CA Certificate is not exported")
		}

		dataPath := "/home/oscar/source/github.com/portainer-updater/dist"
		// dataPath := os.TempDir()
		err := WriteFile(dataPath, nomad.NomadTLSCACertPath, []byte(nomadCACertContent), 0600)
		if err != nil {
			log.Fatal().Err(err).Msg("fail to write the Nomad CA Certificate")
		}

		nomadClientCertContent := envs[nomad.NomadClientCertContentEnvVarName]
		if len(nomadClientCertContent) == 0 {
			log.Fatal().Msg("Nomad Client Certificate is not exported")
		}

		err = WriteFile(dataPath, nomad.NomadTLSCertPath, []byte(nomadClientCertContent), 0600)
		if err != nil {
			log.Fatal().Err(err).Msg("fail to write the Nomad Client Certificate")
		}

		nomadClientKeyContent := envs[nomad.NomadClientKeyContentEnvVarName]
		if len(nomadClientKeyContent) == 0 {
			log.Fatal().Msg("Nomad Client Key is not exported")
		}

		err = WriteFile(dataPath, nomad.NomadTLSKeyPath, []byte(nomadClientKeyContent), 0600)
		if err != nil {
			log.Fatal().Err(err).Msg("fail to write the Nomad Client Key")
		}

		os.Setenv(nomad.NomadCACertEnvVarName, path.Join(dataPath, nomad.NomadTLSCACertPath))
		os.Setenv(nomad.NomadClientCertEnvVarName, path.Join(dataPath, nomad.NomadTLSCertPath))
		os.Setenv(nomad.NomadClientKeyEnvVarName, path.Join(dataPath, nomad.NomadTLSKeyPath))
	}

	nomadCli, err := api.NewClient(api.DefaultConfig())
	if err != nil {
		return errors.WithMessage(err, "failed to initialize Nomad client")
	}

	job, task, err := nomad.FindAgentContainer(ctx, nomadCli)
	if err != nil {
		return errors.WithMessage(err, "failed finding container id")
	}

	return nomad.Update(ctx, nomadCli, job, task, r.Image, r.ScheduleId)
}

func parseEnvs() map[string]string {
	fmt.Println("envs===", os.Environ())
	ret := make(map[string]string)
	for _, env := range os.Environ() {
		nParts := strings.SplitN(env, "=", 2)
		ret[nParts[0]] = nParts[1]
	}
	return ret
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
