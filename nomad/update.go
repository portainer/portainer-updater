package nomad

import (
	"context"

	"github.com/hashicorp/nomad/api"
	"github.com/rs/zerolog/log"
)

func Update(ctx context.Context, nomadCli *api.Client, taskName string, imageName string, scheduleId string) error {

	log.Info().
		Str("image", imageName).
		Str("task", taskName).
		Str("schedule-id", scheduleId).
		Msg("Updating Portainer agent")

	return nil
}
