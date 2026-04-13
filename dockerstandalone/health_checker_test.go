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
	t.Parallel()
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err, "failed to create docker client")
	defer func() {
		err := dockerCli.Close()
		require.NoError(t, err)
	}()

	response := setUpTestContainer(t, dockerCli)

	t.Run("agentHealthy", func(t *testing.T) {
		assert.ErrorIs(t, agentHealthy(t.Context(), dockerCli, response.ID), ErrBinaryNotFound, "should not contain healthy binary, thus should return ErrBinaryNotFound")
	})

	t.Run("portainerHealthy", func(t *testing.T) {
		assert.ErrorIs(t, portainerHealthy(t.Context(), dockerCli, response.ID), ErrFlagNotSupported, "should not contain a binary whose got a health check flag, thus should return ErrFlagNotSupported")
	})
}

func TestExecInContainer_Success(t *testing.T) {
	t.Parallel()
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer func() {
		err := dockerCli.Close()
		require.NoError(t, err)
	}()

	response := setUpTestContainer(t, dockerCli)

	err = execInContainer(t.Context(), dockerCli, response.ID, []string{"echo", "hello"})
	require.NoError(t, err)
}

func TestExecInContainer_NonZeroExitCode(t *testing.T) {
	t.Parallel()
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer func() {
		err := dockerCli.Close()
		require.NoError(t, err)
	}()

	response := setUpTestContainer(t, dockerCli)

	err = execInContainer(t.Context(), dockerCli, response.ID, []string{"sh", "-c", "exit 1"})
	require.Error(t, err)
	require.ErrorContains(t, err, "command failed (1)")
}

func TestHealthyWithCmd_Success(t *testing.T) {
	t.Parallel()
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer func() {
		err := dockerCli.Close()
		require.NoError(t, err)
	}()

	response := setUpTestContainer(t, dockerCli)

	err = healthyWithCmd(t.Context(), dockerCli, response.ID, []string{"echo", "hello"})
	require.NoError(t, err)
}

func TestHealthyWithCmd_NonZeroExitCode(t *testing.T) {
	t.Parallel()
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer func() {
		err := dockerCli.Close()
		require.NoError(t, err)
	}()

	response := setUpTestContainer(t, dockerCli)

	err = healthyWithCmd(t.Context(), dockerCli, response.ID, []string{"sh", "-c", "exit 1"})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrProcessFailedToStart)
}

func TestIsUnknownFlagError(t *testing.T) {
	t.Parallel()
	assert.True(t, isUnknownFlagError(errors.New("unknown long flag '--health-check'")))
	assert.False(t, isUnknownFlagError(errors.New("error")))
	assert.False(t, isUnknownFlagError(nil))
}

// setUpTestContainer creates a test container.
// Note, the container is removed in the test cleanup.
func setUpTestContainer(t *testing.T, dockerCli *client.Client) container.CreateResponse {
	imgRd, err := dockerCli.ImagePull(t.Context(), "busybox:latest", image.PullOptions{})
	require.NoError(t, err)

	_, err = io.Copy(io.Discard, imgRd)
	require.NoError(t, err)
	require.NoError(t, imgRd.Close())

	resp, err := dockerCli.ContainerCreate(t.Context(), &container.Config{
		Image:      "busybox:latest",
		Cmd:        []string{"tail", "-f", "/dev/null"},
		StopSignal: "SIGKILL",
	}, nil, nil, nil, t.Name())
	require.NoError(t, err, "error when creating container")

	t.Cleanup(func() {
		timeout := 5
		// These operations are sensitive to context cancellation, so we use context.WithoutCancel.
		_ = dockerCli.ContainerStop(context.WithoutCancel(t.Context()), resp.ID, container.StopOptions{Timeout: &timeout})
		_ = dockerCli.ContainerRemove(context.WithoutCancel(t.Context()), resp.ID, container.RemoveOptions{Force: true})
	})

	// Start container
	err = dockerCli.ContainerStart(t.Context(), resp.ID, container.StartOptions{})
	require.NoError(t, err, "error when starting container")

	require.Eventually(t, func() bool {
		inspect, err := dockerCli.ContainerInspect(t.Context(), resp.ID)
		if err != nil || !inspect.State.Running {
			return false
		}

		execConfig := container.ExecOptions{Cmd: []string{"true"}}
		execResp, err := dockerCli.ContainerExecCreate(t.Context(), resp.ID, execConfig)
		if err != nil {
			return false
		}

		err = dockerCli.ContainerExecStart(t.Context(), execResp.ID, container.ExecStartOptions{})

		return err == nil

	}, 6*time.Second, 300*time.Millisecond, "container did not become ready in time")

	return resp
}
