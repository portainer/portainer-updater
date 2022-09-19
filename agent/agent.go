package agent

import (
	"github.com/pkg/errors"
	"github.com/portainer/portainer-updater/context"
	"github.com/portainer/portainer-updater/update"
)

type AgentUpdateCommand struct {
	// ContainerID string `arg:"" help:"ID or name of the agent container. e.g. portainer-agent or e9b3e57700ad" name:"container-id"`
	Image      string `arg:"" help:"Image of the agent to upgrade to. e.g. portainer/agent:latest" name:"image"`
	ScheduleId string `arg:"" help:"Schedule ID of the agent to upgrade to. e.g. 1" name:"schedule-id"`
}

func (r *AgentUpdateCommand) Run(cmdCtx *context.CommandExecutionContext) error {
	oldContainerId, err := findContainer(cmdCtx.Context, cmdCtx.DockerCLI)
	if err != nil {
		return errors.WithMessage(err, "failed finding container id")
	}

	return update.Update(oldContainerId, r.Image, r.ScheduleId, cmdCtx)
}
