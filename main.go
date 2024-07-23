package main

import (
	"os"

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

	file, err := os.OpenFile("/tmp/updater.log",
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0664,
	)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	log.ConfigureLogger(cli.CLI.PrettyLog, file)
	log.SetLoggingLevel(log.Level(cli.CLI.LogLevel))

	if err := cliCtx.Run(); err != nil {
		cliCtx.FatalIfErrorf(err)
	}
	os.Exit(0)
}
