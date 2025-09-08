package dockerstandalone

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockDockerClient struct {
	client.APIClient
	t                          *testing.T
	expectedOldContainerID     string
	expectedContainerID        string
	expectedContainerRemoveErr error
	expectedContainerStartErr  error
	expectedContainerLogsErr   error
}

func (c mockDockerClient) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	assert.Equal(c.t, c.expectedContainerID, containerID)

	return c.expectedContainerRemoveErr
}

func (c mockDockerClient) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	assert.Equal(c.t, c.expectedOldContainerID, containerID)

	return c.expectedContainerStartErr
}

func (c mockDockerClient) ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error) {
	assert.Equal(c.t, c.expectedContainerID, containerID)

	return io.NopCloser(bytes.NewReader([]byte("mock log"))), c.expectedContainerLogsErr
}

func TestCleanupContainerAndError(t *testing.T) {
	mockClient := mockDockerClient{
		t:                      t,
		expectedOldContainerID: "mock-old-container-id",
		expectedContainerID:    "mock-container-id",
	}
	t.Run("successful cleanup", func(t *testing.T) {
		err := cleanupContainerAndError(t.Context(), mockClient, mockClient.expectedOldContainerID, mockClient.expectedContainerID)

		require.ErrorIs(t, err, errUpdateFailure)
	})

	t.Run("failed to remove container", func(t *testing.T) {
		mockClient.expectedContainerRemoveErr = errors.New("failed to remove container")
		
		err := cleanupContainerAndError(t.Context(), mockClient, mockClient.expectedOldContainerID, mockClient.expectedContainerID)

		require.ErrorIs(t, err, errUpdateFailure)
	})

	t.Run("failed to start old container", func(t *testing.T) {
		mockClient.expectedContainerRemoveErr = nil
		mockClient.expectedContainerStartErr = errors.New("failed to start old container")

		err := cleanupContainerAndError(t.Context(), mockClient, mockClient.expectedOldContainerID, mockClient.expectedContainerID)

		require.ErrorIs(t, err, errUpdateFailure)
	})

	t.Run("failed to get logs of failed container", func(t *testing.T) {
		mockClient.expectedContainerStartErr = nil
		mockClient.expectedContainerLogsErr = errors.New("failed to get logs of failed container")

		err := cleanupContainerAndError(t.Context(), mockClient, mockClient.expectedOldContainerID, mockClient.expectedContainerID)

		require.ErrorIs(t, err, errUpdateFailure)
	})
}

func TestMonitorExtendedHealth(t *testing.T) {
	t.Run("failed to verify health", func(t *testing.T) {

		testHealthCheck := func(ctx context.Context, cli *client.Client, containerID string) error {
			return errors.New("failed to verify health")
		}
		backoffBase := 5 // seconds
		expectedElapsedSeconds := func() int {
			sum := 0
			for i := range 10 {
				sum += i * backoffBase
			}

			return sum
		}()

		synctest.Test(t, func(t *testing.T) {
			now := time.Now()

			healthy, err := monitorExtendedHealth(t.Context(), nil, "id", testHealthCheck, backoffBase, "test")

			require.False(t, healthy)
			require.ErrorContains(t, err, "test health check timed out")
			elapsedSeconds := int(time.Since(now).Seconds())
			require.InDelta(t, expectedElapsedSeconds, elapsedSeconds, 2, "elapsedSeconds should be within 2 seconds of expectedElapsedSeconds")
		})
	})

	t.Run("verify health succeeds after retries", func(t *testing.T) {
		calls := 0
		testHealthCheck := func(ctx context.Context, cli *client.Client, containerID string) error {
			calls++
			if calls == 3 {
				return nil // succeed on 3rd attempt
			}
			return errors.New("failed to verify health")
		}
		backOffBase := 5
		synctest.Test(t, func(t *testing.T) {
			healthy, err := monitorExtendedHealth(t.Context(), nil, "id", testHealthCheck, backOffBase, "test")

			require.NoError(t, err, "should not return error when health check eventually succeeds")
			require.True(t, healthy, "should be healthy when health check eventually succeeds")
			require.Equal(t, 3, calls, "should call health check 3 times")
		})
	})
}

func TestUpdate_monitorAgentHealthMissingBinary(t *testing.T) {
	ctx := context.Background()

	var logBuffer bytes.Buffer
	log.Logger = log.Output(&logBuffer)

	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer dockerCli.Close()

	response := setUpTestContainer(t, ctx, dockerCli)

	ok, err := monitorAgentHealth(ctx, dockerCli, response.ID, false)

	require.NoError(t, err, "should not return error when the healthy binary is missing")
	assert.True(t, ok, "should be true because the healthy binary is missing and the agent health is thereby assumed to be ok")
	assert.Contains(t, logBuffer.String(), "Agent health cannot be checked. Assuming health check passed.", "should contain the log message about the missing healthy binary")
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
