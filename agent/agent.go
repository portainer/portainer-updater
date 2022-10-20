package agent

import (
	"github.com/pkg/errors"
	"github.com/portainer/portainer-updater/context"
	"github.com/portainer/portainer-updater/update"
	"github.com/rs/zerolog/log"
)

type AgentUpdateCommand struct {
	Image      string `arg:"" help:"Image of the agent to upgrade to. e.g. portainer/agent:latest" name:"image"`
	ScheduleId string `arg:"" help:"Schedule ID of the agent to upgrade to. e.g. 1" name:"schedule-id"`
}

func (r *AgentUpdateCommand) Run(cmdCtx *context.CommandExecutionContext) error {
	log.Info().
		Str("image", r.Image).
		Str("schedule-id", r.ScheduleId).
		Msg("Updating Portainer agent")
	oldContainer, err := findContainer(cmdCtx.Context, cmdCtx.DockerCLI)
	if err != nil {
		return errors.WithMessage(err, "failed finding container id")
	}

	if oldContainer.Labels != nil && oldContainer.Labels[update.UpdateScheduleIDLabel] == r.ScheduleId {
		log.Info().Msg("Agent already updated")

		return nil
	}

	return update.Update(oldContainer.ID, r.Image, r.ScheduleId, cmdCtx)
}
