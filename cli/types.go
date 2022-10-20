package cli

import (
	"github.com/portainer/portainer-updater/agent"
	"github.com/portainer/portainer-updater/log"
)

var CLI struct {
	LogLevel  log.Level          `kong:"help='Set the logging level',default='INFO',enum='DEBUG,INFO,WARN,ERROR',env='LOG_LEVEL'"`
	PrettyLog bool               `kong:"help='Whether to enable or disable colored logs output',default='false',env='PRETTY_LOG'"`
	Agent     agent.AgentCommand `cmd:"" help:"Update an existing Portainer agent container."`
}
