package dockerstandalone

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/portainer/portainer-updater/logs"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockDockerClient struct {
	client.APIClient
	t                            *testing.T
	expectedOldContainerID       string
	expectInspectOldContainerID  bool
	expectedContainerID          string
	expectedRollbackContainerID  string
	expectedContainerRemoveErr   error
	expectedContainerStartErr    error
	expectedContainerLogsErr     error
	expectedContainerInspectResp *container.InspectResponse
	expectedContainerInspectErr  error

	containerWaitErrChannel  <-chan error
	containerWaitRespChannel <-chan container.WaitResponse
}

func (c mockDockerClient) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	if strings.Contains(containerID, "rollback") {
		assert.Equal(c.t, c.expectedRollbackContainerID, containerID)
	} else {
		assert.Equal(c.t, c.expectedContainerID, containerID)
	}

	return c.expectedContainerRemoveErr
}

func (c mockDockerClient) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	if strings.Contains(containerID, "rollback") {
		assert.Equal(c.t, c.expectedRollbackContainerID, containerID)
	} else {
		assert.Equal(c.t, c.expectedOldContainerID, containerID)
	}

	return c.expectedContainerStartErr
}

func (c mockDockerClient) ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error) {
	assert.Equal(c.t, c.expectedContainerID, containerID)

	return io.NopCloser(bytes.NewReader([]byte("mock log"))), c.expectedContainerLogsErr
}

func (c mockDockerClient) ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error) {
	if c.expectInspectOldContainerID {
		assert.Equal(c.t, c.expectedOldContainerID, containerID)
	} else {
		assert.Equal(c.t, c.expectedContainerID, containerID)
	}

	if c.expectedContainerInspectResp != nil {
		return *c.expectedContainerInspectResp, c.expectedContainerInspectErr
	}

	return container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			ID:   containerID,
			Name: "test-container",
		},
		Config:          &container.Config{},
		NetworkSettings: &container.NetworkSettings{},
	}, c.expectedContainerInspectErr
}

func (c mockDockerClient) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
	return container.CreateResponse{
		ID: c.expectedRollbackContainerID,
	}, nil
}

func (c mockDockerClient) ContainerWait(ctx context.Context, container string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	return c.containerWaitRespChannel, c.containerWaitErrChannel
}

func (c mockDockerClient) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	assert.Equal(c.t, c.expectedContainerID, containerID)

	return nil
}

func TestMonitorHealth(t *testing.T) {
	t.Parallel()
	t.Run("should return early if no healthcheck is set up", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			expectedContainerID := "test-container-id"
			mockClient := mockDockerClient{
				t:                   t,
				expectedContainerID: expectedContainerID,
				expectedContainerInspectResp: &container.InspectResponse{
					ContainerJSONBase: &container.ContainerJSONBase{
						ID:    expectedContainerID,
						Name:  "test-container",
						State: &container.State{},
					},
				},
			}
			assertLogs := withLogAssertions(t, "No health check found for the container. Assuming health check passed")

			ok, err := monitorHealth(t.Context(), mockClient, expectedContainerID)

			require.NoError(t, err)
			require.True(t, ok, "should be true if no healthcheck is set up")
			assertLogs()
		})
	})
	t.Run("failed to assert healthiness by timeout", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			start := time.Now()
			expectedContainerID := "test-container-id"
			mockClient := mockDockerClient{
				t:                   t,
				expectedContainerID: expectedContainerID,
				expectedContainerInspectResp: &container.InspectResponse{
					ContainerJSONBase: &container.ContainerJSONBase{
						ID:   expectedContainerID,
						Name: "test-container",
						State: &container.State{
							Health: &container.Health{
								Status: container.Starting,
							},
						},
					},
				},
			}
			assertLogs := withLogAssertions(t, "Health check timed out. Exiting without updating the container")

			ok, err := monitorHealth(t.Context(), mockClient, expectedContainerID)

			require.NoError(t, err)
			require.False(t, ok, "should be false when health check fails")
			require.WithinRange(t, time.Now(), start.Add(35*time.Second), start.Add(45*time.Second), "should take ~40s")

			assertLogs()
		})
	})

	t.Run("failed to assert healthiness by status ", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			expectedContainerID := "test-container-id"
			mockClient := mockDockerClient{
				t:                   t,
				expectedContainerID: expectedContainerID,
				expectedContainerInspectResp: &container.InspectResponse{
					ContainerJSONBase: &container.ContainerJSONBase{
						ID:   expectedContainerID,
						Name: "test-container",
						State: &container.State{
							Health: &container.Health{
								Status: container.Unhealthy,
							},
						},
					},
				},
			}
			assertLogs := withLogAssertions(t, "Health check failed. Exiting without updating the container")

			ok, err := monitorHealth(t.Context(), mockClient, expectedContainerID)

			require.NoError(t, err)
			require.False(t, ok, "should be false when health check fails")

			assertLogs()
		})
	})

}

func TestCleanupContainerAndError(t *testing.T) {
	t.Parallel()
	mockClient := mockDockerClient{
		t:                      t,
		expectedOldContainerID: "mock-old-container-id",
		expectedContainerID:    "mock-container-id",
	}
	t.Run("successful cleanup", func(t *testing.T) {
		assertLogs := withLogAssertions(t,
			"An error occurred during the update process - removing newly created container",
			"Printing container logs to stdout",
			"Successfully restarted old container and cleaned up temporary container",
		)

		err := cleanupContainerAndError(t.Context(), mockClient, mockClient.expectedOldContainerID, mockClient.expectedContainerID, false)

		require.ErrorIs(t, err, errUpdateFailure)
		assertLogs()
	})
	t.Run("successful cleanup with successful db rollback", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			start := time.Now()

			assertLogs := withLogAssertions(t,
				"An error occurred during the update process - removing newly created container",
				"Printing container logs to stdout",
				"Database rollback completed successfully",
				"Successfully restarted old container and cleaned up temporary container",
			)

			rollbackDuration := 2 * time.Minute
			containerWaitRespCh := make(chan container.WaitResponse)
			go func() {
				time.Sleep(rollbackDuration)
				containerWaitRespCh <- container.WaitResponse{StatusCode: 0}
				close(containerWaitRespCh)
			}()

			customMockClient := mockDockerClient{
				t:                           t,
				expectedOldContainerID:      "mock-old-container-id",
				expectedContainerID:         "mock-container-id",
				expectedRollbackContainerID: "mock-rollback-container-id",
				expectInspectOldContainerID: true,
				containerWaitErrChannel:     make(chan error, 1),
				containerWaitRespChannel:    containerWaitRespCh,
			}

			err := cleanupContainerAndError(t.Context(), customMockClient, customMockClient.expectedOldContainerID, customMockClient.expectedContainerID, true)
			synctest.Wait()
			require.ErrorIs(t, err, errUpdateFailure)

			require.WithinRange(t, time.Now(), start, start.Add(rollbackDuration+1*time.Second))
			assertLogs()
		})
	})

	t.Run("successful cleanup with successful db rollback but failing container removal", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			start := time.Now()

			assertLogs := withLogAssertions(t,
				"An error occurred during the update process - removing newly created container",
				"Printing container logs to stdout",
				"unable to remove rollback container",
				"Unable to rollback database changes. Manual rollback might be required",
			)

			rollbackDuration := 2 * time.Minute
			containerWaitRespCh := make(chan container.WaitResponse)
			go func() {
				time.Sleep(rollbackDuration)
				containerWaitRespCh <- container.WaitResponse{StatusCode: 0}
				close(containerWaitRespCh)
			}()

			customMockClient := mockDockerClient{
				t:                           t,
				expectedOldContainerID:      "mock-old-container-id",
				expectedContainerID:         "mock-container-id",
				expectedRollbackContainerID: "mock-rollback-container-id",
				expectInspectOldContainerID: true,
				containerWaitErrChannel:     make(chan error, 1),
				containerWaitRespChannel:    containerWaitRespCh,
				expectedContainerRemoveErr:  errors.New("test error"),
			}

			err := cleanupContainerAndError(t.Context(), customMockClient, customMockClient.expectedOldContainerID, customMockClient.expectedContainerID, true)
			synctest.Wait()
			require.ErrorIs(t, err, errUpdateFailure)

			require.WithinRange(t, time.Now(), start, start.Add(rollbackDuration+1*time.Second))
			assertLogs()
		})
	})

	t.Run("successful cleanup with db rollback process failure", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			start := time.Now()

			assertLogs := withLogAssertions(t,
				"An error occurred during the update process - removing newly created container",
				"Printing container logs to stdout",
				"Unable to rollback database changes. Manual rollback might be required",
				"Successfully restarted old container and cleaned up temporary container",
			)

			rollbackDuration := 2 * time.Minute
			containerWaitRespCh := make(chan container.WaitResponse)
			go func() {
				time.Sleep(rollbackDuration)
				containerWaitRespCh <- container.WaitResponse{StatusCode: 1, Error: &container.WaitExitError{Message: "mock rollback error"}}
				close(containerWaitRespCh)
			}()

			customMockClient := mockDockerClient{
				t:                           t,
				expectedOldContainerID:      "mock-old-container-id",
				expectedContainerID:         "mock-container-id",
				expectedRollbackContainerID: "mock-rollback-container-id",
				expectInspectOldContainerID: true,
				containerWaitErrChannel:     make(chan error, 1),
				containerWaitRespChannel:    containerWaitRespCh,
			}

			err := cleanupContainerAndError(t.Context(), customMockClient, customMockClient.expectedOldContainerID, customMockClient.expectedContainerID, true)
			synctest.Wait()
			require.ErrorIs(t, err, errUpdateFailure)

			require.WithinRange(t, time.Now(), start, start.Add(rollbackDuration+1*time.Second))
			assertLogs()
		})
	})

	t.Run("successful cleanup with db rollback container failure", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			start := time.Now()

			assertLogs := withLogAssertions(t,
				"An error occurred during the update process - removing newly created container",
				"Printing container logs to stdout",
				"Unable to rollback database changes. Manual rollback might be required",
				"waiting for container failed",
				"Successfully restarted old container and cleaned up temporary container",
			)

			rollbackDuration := 2 * time.Minute
			containerWaitErr := make(chan error, 1)
			go func() {
				time.Sleep(rollbackDuration)
				containerWaitErr <- errors.New("mock container wait error")
				close(containerWaitErr)
			}()

			customMockClient := mockDockerClient{
				t:                           t,
				expectedOldContainerID:      "mock-old-container-id",
				expectedContainerID:         "mock-container-id",
				expectedRollbackContainerID: "mock-rollback-container-id",
				expectInspectOldContainerID: true,
				containerWaitErrChannel:     containerWaitErr,
				containerWaitRespChannel:    make(chan container.WaitResponse),
			}

			err := cleanupContainerAndError(t.Context(), customMockClient, customMockClient.expectedOldContainerID, customMockClient.expectedContainerID, true)
			synctest.Wait()
			require.ErrorIs(t, err, errUpdateFailure)

			require.WithinRange(t, time.Now(), start, start.Add(rollbackDuration+1*time.Second))
			assertLogs()
		})
	})

	t.Run("successful cleanup with db rollback timeout", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			start := time.Now()

			assertLogs := withLogAssertions(t,
				"An error occurred during the update process - removing newly created container",
				"Printing container logs to stdout",
				"Unable to rollback database changes. Manual rollback might be required",
				"Successfully restarted old container and cleaned up temporary container",
				"timeout waiting for rollback container to finish",
			)

			customMockClient := mockDockerClient{
				t:                           t,
				expectedOldContainerID:      "mock-old-container-id",
				expectedContainerID:         "mock-container-id",
				expectedRollbackContainerID: "mock-rollback-container-id",
				expectInspectOldContainerID: true,
				containerWaitErrChannel:     make(chan error, 1),               // never sending anything to simulate timeout
				containerWaitRespChannel:    make(chan container.WaitResponse), // never sending anything to simulate timeout
			}

			err := cleanupContainerAndError(t.Context(), customMockClient, customMockClient.expectedOldContainerID, customMockClient.expectedContainerID, true)
			synctest.Wait()
			require.ErrorIs(t, err, errUpdateFailure)

			require.WithinRange(t, time.Now(), start, start.Add(6*time.Minute), "should timeout within 5 minutes")
			assertLogs()
		})
	})

	t.Run("failed to remove container", func(t *testing.T) {
		mockClient.expectedContainerRemoveErr = errors.New("failed to remove container")

		assertLogs := withLogAssertions(t,
			"An error occurred during the update process - removing newly created container",
			"Printing container logs to stdout",
			"Unable to remove temporary container, please remove it manually",
			"Successfully restarted old container and cleaned up temporary container",
		)

		err := cleanupContainerAndError(t.Context(), mockClient, mockClient.expectedOldContainerID, mockClient.expectedContainerID, false)

		require.ErrorIs(t, err, errUpdateFailure)
		assertLogs()
	})

	t.Run("failed to start old container", func(t *testing.T) {
		mockClient.expectedContainerRemoveErr = nil
		mockClient.expectedContainerStartErr = errors.New("failed to start old container")

		assertLogs := withLogAssertions(t,
			"An error occurred during the update process - removing newly created container",
			"Printing container logs to stdout",
			"Unable to restart container, please restart it manually",
			"Successfully restarted old container and cleaned up temporary container",
		)

		err := cleanupContainerAndError(t.Context(), mockClient, mockClient.expectedOldContainerID, mockClient.expectedContainerID, false)

		require.ErrorIs(t, err, errUpdateFailure)
		assertLogs()
	})

	t.Run("failed to get logs of failed container", func(t *testing.T) {
		mockClient.expectedContainerStartErr = nil
		mockClient.expectedContainerLogsErr = errors.New("failed to get logs of failed container")

		assertLogs := withLogAssertions(t,
			"An error occurred during the update process - removing newly created container",
			"Printing container logs to stdout",
			"Unable to get container logs",
			"Successfully restarted old container and cleaned up temporary container",
		)

		err := cleanupContainerAndError(t.Context(), mockClient, mockClient.expectedOldContainerID, mockClient.expectedContainerID, false)

		require.ErrorIs(t, err, errUpdateFailure)
		assertLogs()
	})
}

func TestMonitorExtendedHealth(t *testing.T) {
	t.Parallel()
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

			healthy, err := monitorExtendedHealth(t.Context(), nil, "id", testHealthCheck, backoffBase, "test", 10)

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
			healthy, err := monitorExtendedHealth(t.Context(), nil, "id", testHealthCheck, backOffBase, "test", 10)

			require.NoError(t, err, "should not return error when health check eventually succeeds")
			require.True(t, healthy, "should be healthy when health check eventually succeeds")
			require.Equal(t, 3, calls, "should call health check 3 times")
		})
	})
}

func TestUpdate_monitorAgentHealthMissingBinary(t *testing.T) {
	t.Parallel()
	assertLogs := withLogAssertions(t, "Agent health cannot be checked. Assuming health check passed.")

	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer logs.CloseAndLogErr(dockerCli)

	response := setUpTestContainer(t, dockerCli)

	ok, err := monitorAgentHealth(t.Context(), dockerCli, response.ID, false)

	require.NoError(t, err, "should not return error when the healthy binary is missing")
	assert.True(t, ok, "should be true because the healthy binary is missing and the agent health is thereby assumed to be ok")
	assertLogs()
}

func TestBuildContainerName(t *testing.T) {
	t.Parallel()
	require.Equal(t, "x-update", buildContainerName("x"))
	require.Equal(t, "x", buildContainerName("x-update"))
}

func withLogAssertions(t *testing.T, logs ...string) func() {
	oldLogger := log.Logger

	var logBuffer bytes.Buffer
	log.Logger = log.Output(&logBuffer)

	t.Cleanup(func() {
		log.Logger = oldLogger
	})

	return func() {
		logContent := logBuffer.String()
		for _, logEntry := range logs {
			assert.Contains(t, logContent, logEntry)
		}
	}
}
