package dockerstandalone

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthy(t *testing.T) {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err, "failed to create docker client")
	defer func() {
		err := dockerCli.Close()
		require.NoError(t, err)
	}()

	response := setUpTestContainer(t, t.Context(), dockerCli)

	t.Run("agentHealthy", func(t *testing.T) {
		assert.ErrorIs(t, agentHealthy(t.Context(), dockerCli, response.ID), ErrBinaryNotFound, "should not contain healthy binary, thus should return ErrBinaryNotFound")
	})

	t.Run("portainerHealthy", func(t *testing.T) {
		assert.ErrorIs(t, portainerHealthy(t.Context(), dockerCli, response.ID), ErrFlagNotSupported, "should not contain a binary whose got a health check flag, thus should return ErrFlagNotSupported")
	})
}

func TestIsUnknownFlagError(t *testing.T) {
	assert.True(t, isUnknownFlagError(errors.New("unknown long flag '--health-check'")))
	assert.False(t, isUnknownFlagError(errors.New("error")))
	assert.False(t, isUnknownFlagError(nil))
}

// setUpTestContainer creates a test container.
// Note, the container is removed in the test cleanup.
func setUpTestContainer(t *testing.T, ctx context.Context, dockerCli *client.Client) container.CreateResponse {
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
		// These operations are sensitive to context cancellation, so we use context.WithoutCancel.
		_ = dockerCli.ContainerStop(context.WithoutCancel(ctx), resp.ID, container.StopOptions{Timeout: &timeout})
		_ = dockerCli.ContainerRemove(context.WithoutCancel(ctx), resp.ID, container.RemoveOptions{Force: true})
	})

	// Start container
	err = dockerCli.ContainerStart(ctx, resp.ID, container.StartOptions{})
	require.NoError(t, err, "error when starting container")

	require.Eventually(t, func() bool {
		inspect, err := dockerCli.ContainerInspect(ctx, resp.ID)
		if err != nil || !inspect.State.Running {
			return false
		}

		execConfig := container.ExecOptions{Cmd: []string{"true"}}
		execResp, err := dockerCli.ContainerExecCreate(ctx, resp.ID, execConfig)
		if err != nil {
			return false
		}

		err = dockerCli.ContainerExecStart(ctx, execResp.ID, container.ExecStartOptions{})

		return err == nil

	}, 6*time.Second, 300*time.Millisecond, "container did not become ready in time")

	return resp
}
