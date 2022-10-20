package context

import (
	"context"

	"github.com/docker/docker/client"
)

type CommandExecutionContext struct {
	Context   context.Context
	DockerCLI *client.Client
}

func NewCommandExecutionContext(ctx context.Context, dockerCli *client.Client) *CommandExecutionContext {
	return &CommandExecutionContext{
		Context:   ctx,
		DockerCLI: dockerCli,
	}
}
