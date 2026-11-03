package main

import (
	"os"

	"github.com/alecthomas/kong"
	"github.com/portainer/portainer-updater/cli"
	"github.com/portainer/portainer-updater/logs"
	"github.com/rs/zerolog/log"
)

// Kubernetes copies this file into the pod status when the container exits, so the server can show why the update failed.
const terminationMessagePath = "/dev/termination-log"

func main() {
	cliCtx := kong.Parse(&cli.CLI,
		kong.Name("portainer-updater"),
		kong.Description("A tool to update Portainer software"),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact: true,
			Summary: true,
		}))

	logs.ConfigureLogger(cli.CLI.PrettyLog)
	logs.SetLoggingLevel(cli.CLI.LogLevel)

	err := cliCtx.Run()
	if err != nil {
		writeTerminationMessage(terminationMessagePath, err)
		cliCtx.FatalIfErrorf(err)
	}

	os.Exit(0)
}

// writeTerminationMessage does nothing outside Kubernetes, where the file doesn't exist.
func writeTerminationMessage(path string, err error) {
	if _, statErr := os.Stat(path); statErr != nil {
		return
	}

	if writeErr := os.WriteFile(path, []byte(err.Error()), 0o600); writeErr != nil {
		log.Warn().Err(writeErr).Msg("Unable to write the termination message")
	}
}
