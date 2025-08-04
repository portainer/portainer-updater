package dockerstandalone

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdate_monitorAgentHealthMissingBinary(t *testing.T) {
	ctx := context.Background()

	var logBuffer bytes.Buffer
	log.Logger = log.Output(&logBuffer)

	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer dockerCli.Close()

	response := setUpAgentContainerWithoutHealthyBinary(t, ctx, dockerCli)

	ok, err := monitorAgentHealth(ctx, dockerCli, response.ID, false)

	assert.NoError(t, err, "should not return error when the healthy binary is missing")
	assert.True(t, ok, "should be true because the healthy binary is missing and the agent health is thereby assumed to be ok")
	assert.Contains(t, logBuffer.String(), "Agent health cannot be checked. Assuming health check passed.", "should contain the log message about the missing healthy binary")
}

// setUpAgentContainerWithoutHealthyBinary creates a container without the healthy binary and returns its ID.
// Note, the container is removed in the test cleanup.
func setUpAgentContainerWithoutHealthyBinary(t *testing.T, ctx context.Context, dockerCli *client.Client) container.CreateResponse {
	imgRd, err := dockerCli.ImagePull(ctx, "busybox:latest", image.PullOptions{})
	require.NoError(t, err)

	_, err = io.Copy(io.Discard, imgRd)
	require.NoError(t, err)
	require.NoError(t, imgRd.Close())

	resp, err := dockerCli.ContainerCreate(ctx, &container.Config{
		Image:      "busybox:latest",
		Cmd:        []string{"tail", "-f", "/dev/null"},
		StopSignal: "SIGKILL",
	}, nil, nil, nil, t.Name())
	require.NoError(t, err, "error when creating container")

	t.Cleanup(func() {
		timeout := 5
		_ = dockerCli.ContainerStop(ctx, resp.ID, container.StopOptions{Timeout: &timeout})
		_ = dockerCli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
	})

	// Start container
	err = dockerCli.ContainerStart(ctx, resp.ID, container.StartOptions{})
	require.NoError(t, err, "error when starting container")

	// Inspect container to verify env vars
	inspect, err := dockerCli.ContainerInspect(ctx, resp.ID)
	require.NoError(t, err, "error when inspecting container")

	for range 10 {
		if inspect.State.Running {
			break
		}

		time.Sleep(300 * time.Millisecond)
	}

	require.True(t, inspect.State.Running)

	return resp
}

func TestBuildContainerName(t *testing.T) {
	require.Equal(t, "x-update", buildContainerName("x"))
	require.Equal(t, "x", buildContainerName("x-update"))
}
