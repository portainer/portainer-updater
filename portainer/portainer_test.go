package portainer

import (
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"
)

func TestPortainerInvalidEnvType(t *testing.T) {
	command := Command{
		EnvType: "invalid-env-type",
	}

	err := command.Run()
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown environment type: invalid-env-type")
}

func TestCommand_Defaults(t *testing.T) {
	var command Command
	cli, err := kong.New(&command)
	require.NoError(t, err)

	_, err = cli.Parse([]string{}) // no args, just defaults
	require.NoError(t, err)

	require.Equal(t, EnvTypeDockerStandalone, command.EnvType)
	require.Equal(t, "portainer/portainer-ee:latest", command.Image)
	require.False(t, command.HealthCheck)
	require.Empty(t, command.License)
}

func TestCommandHealthCheckFlag(t *testing.T) {
	var command Command
	cli, err := kong.New(&command)
	require.NoError(t, err)
	cases := []struct {
		args           []string
		expectedHealth bool
	}{
		{[]string{}, false},
		{[]string{"--health-check"}, true},
		{[]string{"--health-check=true"}, true},
		{[]string{"--health-check=false"}, false},
		{[]string{"--health-check=0"}, false},
		{[]string{"--health-check=1"}, true},
	}

	for _, c := range cases {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			_, err = cli.Parse(c.args)
			require.NoError(t, err)
			require.Equal(t, c.expectedHealth, command.HealthCheck)
		})
	}
}
