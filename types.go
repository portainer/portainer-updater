package main

import (
	"context"

	"github.com/docker/docker/client"
	"go.uber.org/zap"
)

type CommandExecutionContext struct {
	context   context.Context
	logger    *zap.SugaredLogger
	dockerCLI *client.Client
}

func NewCommandExecutionContext(ctx context.Context, logger *zap.SugaredLogger, dockerCli *client.Client) *CommandExecutionContext {
	return &CommandExecutionContext{
		context:   ctx,
		logger:    logger,
		dockerCLI: dockerCli,
	}
}

var cli struct {
	Debug bool `help:"Enable debug mode."`

	AgentUpdate AgentUpdateCommand `cmd:"" help:"Update an existing Portainer agent container."`
}
