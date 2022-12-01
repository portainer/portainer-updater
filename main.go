package main

import (
	"github.com/alecthomas/kong"
	"github.com/portainer/portainer-updater/cli"
	"github.com/portainer/portainer-updater/log"
)

func main() {

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

	err := cliCtx.Run()
	cliCtx.FatalIfErrorf(err)
}
