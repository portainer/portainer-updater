package cli

import (
	"github.com/portainer/portainer-updater/agent"
)

var CLI struct {
	Debug bool `help:"Enable debug mode."`

	AgentUpdate agent.AgentUpdateCommand `cmd:"" help:"Update an existing Portainer agent container."`
}
