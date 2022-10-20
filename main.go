package main

import (
	gocontext "context"

	zerolog "github.com/rs/zerolog/log"

	"github.com/alecthomas/kong"
	"github.com/docker/docker/client"
	"github.com/portainer/portainer-updater/cli"
	"github.com/portainer/portainer-updater/context"
	"github.com/portainer/portainer-updater/log"
)

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

	log.ConfigureLogger(cli.CLI.PrettyLog)
	log.SetLoggingLevel(log.Level(cli.CLI.LogLevel))

	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		zerolog.Fatal().Err(err).Msg("Unable to initialize Docker client")
	}

	cmdCtx := context.NewCommandExecutionContext(ctx, dockerCli)
	err = cliCtx.Run(cmdCtx)
	cliCtx.FatalIfErrorf(err)
}
