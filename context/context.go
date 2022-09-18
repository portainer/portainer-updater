package context

import (
	"context"

	"github.com/docker/docker/client"
	"go.uber.org/zap"
)

type CommandExecutionContext struct {
	Context   context.Context
	Logger    *zap.SugaredLogger
	DockerCLI *client.Client
}

func NewCommandExecutionContext(ctx context.Context, logger *zap.SugaredLogger, dockerCli *client.Client) *CommandExecutionContext {
	return &CommandExecutionContext{
		Context:   ctx,
		Logger:    logger,
		DockerCLI: dockerCli,
	}
}
