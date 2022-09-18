package main

import (
	gocontext "context"
	"log"

	"github.com/alecthomas/kong"
	"github.com/docker/docker/client"
	"github.com/portainer/portainer-updater/cli"
	"github.com/portainer/portainer-updater/context"
	"go.uber.org/zap"
)

func initializeLogger(debug bool) (*zap.SugaredLogger, error) {
	if debug {
		logger, err := zap.NewDevelopment()
		if err != nil {
			return nil, err
		}

		return logger.Sugar(), nil
	}

	logger, err := zap.NewProduction()
	if err != nil {
		return nil, err
	}

	return logger.Sugar(), nil
}

func main() {
	ctx := gocontext.Background()

	cliCtx := kong.Parse(&cli.CLI,
		kong.Name("portainer-updater"),
		kong.Description("A tool to update Portainer software"),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact: true,
			Summary: true,
		}))

	logger, err := initializeLogger(cli.CLI.Debug)
	if err != nil {
		log.Fatalf("Unable to initialize logger: %s", err)
	}

	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		logger.Fatalw("Unable to create Docker client", "error", err)
	}

	cmdCtx := context.NewCommandExecutionContext(ctx, logger, dockerCli)
	err = cliCtx.Run(cmdCtx)
	cliCtx.FatalIfErrorf(err)
}
