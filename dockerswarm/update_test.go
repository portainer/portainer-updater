package dockerswarm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateSameTag(t *testing.T) {
	dockerClient := &mockDockerClient{}
	withPullImage(dockerClient, nil, true)
	swarmService := setUpSwarmService()
	imageName := swarmService.Spec.TaskTemplate.ContainerSpec.Image

	err := Update(t.Context(), dockerClient, imageName, swarmService, nil, UpdateOptions{})

	require.NoError(t, err, "should not return an error as the image is up to date, and no update is required")
}

func TestUpdateVersionIncrement(t *testing.T) {
	t.Setenv("SKIP_PULL", "true")

	swarmService := &swarm.Service{
		ID: "swarm-id",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Image: "image-name",
				},
			},
		},
		Meta: swarm.Meta{
			Version: swarm.Version{Index: 1},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dockerCli, err := client.NewClientWithOpts(client.WithHost(srv.URL), client.WithHTTPClient(http.DefaultClient))
	require.NoError(t, err)

	err = Update(context.Background(), dockerCli, "image-name", swarmService, func(*swarm.ContainerSpec) {}, UpdateOptions{})
	require.Error(t, err)
	require.Equal(t, uint64(2), swarmService.Version.Index)
}

func TestUpdateWithExtendedHealthCheck(t *testing.T) {
	t.Setenv("SKIP_PULL", "true")

	imageName := "image-name"

	defaultAssertServiceUpdate := func(t *testing.T, service swarm.ServiceSpec) {
		require.Equal(t, imageName, service.TaskTemplate.ContainerSpec.Image)
		require.NotNil(t, service.TaskTemplate.ContainerSpec.Healthcheck)
		require.Equal(t, []string{"CMD", "/portainer", "--health-check"}, service.TaskTemplate.ContainerSpec.Healthcheck.Test)
	}

	t.Run("successful update", func(t *testing.T) {
		swarmService := setUpSwarmService()

		dockerClient := &mockDockerClient{
			t:                   t,
			assertServiceUpdate: defaultAssertServiceUpdate,
			updateStates:        []swarm.UpdateState{swarm.UpdateStateCompleted},
		}

		err := Update(context.Background(), dockerClient, imageName, swarmService, func(*swarm.ContainerSpec) {}, UpdateOptions{
			ExtendedHealthCheck: true,
		})

		require.NoError(t, err)
		require.Equal(t, uint64(2), swarmService.Version.Index)
	})

	t.Run("failed to update service because of timeout", func(t *testing.T) {
		swarmService := setUpSwarmService()

		dockerClient := &mockDockerClient{
			t:                   t,
			assertServiceUpdate: defaultAssertServiceUpdate,
			errServiceUpdate:    nil,
			updateStates:        []swarm.UpdateState{swarm.UpdateStateUpdating},
		}

		synctest.Test(t, func(t *testing.T) {
			start := time.Now()
			err := Update(context.Background(), dockerClient, imageName, swarmService, func(*swarm.ContainerSpec) {}, UpdateOptions{
				ExtendedHealthCheck: true,
			})
			end := time.Now()

			require.ErrorIs(t, err, errUpdateFailure)

			expectedDuration := 3 * time.Hour
			expectedDurationWithMargin := expectedDuration + 1*time.Second

			require.WithinDuration(t, start, end, expectedDurationWithMargin)
		})
	})

	t.Run("failed to update because of rollback", func(t *testing.T) {
		swarmService := setUpSwarmService()

		var updateStatuses []swarm.UpdateState
		for range 10 {
			updateStatuses = append(updateStatuses, swarm.UpdateStateUpdating)
		}
		for range 5 {
			updateStatuses = append(updateStatuses, swarm.UpdateStateRollbackStarted)
		}
		updateStatuses = append(updateStatuses, swarm.UpdateStateRollbackCompleted)

		waitRespCh := make(chan container.WaitResponse, 1)
		waitRespCh <- container.WaitResponse{StatusCode: 0}
		close(waitRespCh)
		dockerClient := &mockDockerClient{
			t:                        t,
			assertServiceUpdate:      defaultAssertServiceUpdate,
			errServiceUpdate:         nil,
			updateStates:             updateStatuses,
			expectedContainerID:      "container-id",
			containerWaitRespChannel: waitRespCh,
			taskListCalls: [][]swarm.Task{
				{
					{Status: swarm.TaskStatus{State: swarm.TaskStateRunning}},
				},
				{
					{Status: swarm.TaskStatus{State: swarm.TaskStateShutdown}},
				},
				{
					{Status: swarm.TaskStatus{State: swarm.TaskStateStarting}},
				},
				{
					{Status: swarm.TaskStatus{State: swarm.TaskStateRunning}},
				},
			},
		}

		synctest.Test(t, func(t *testing.T) {
			start := time.Now()

			err := Update(context.Background(), dockerClient, imageName, swarmService, func(*swarm.ContainerSpec) {}, UpdateOptions{
				ExtendedHealthCheck: true,
			})
			end := time.Now()

			require.ErrorIs(t, err, errUpdateFailure)

			expectedDuration := time.Duration(len(updateStatuses)) * 5 * time.Second
			expectedDurationWithMargin := expectedDuration + 1*time.Second

			require.WithinDuration(t, start, end, expectedDurationWithMargin, "should return after %d seconds", int(expectedDuration.Seconds()))
		})
	})
}

func TestPullImageSkipPull(t *testing.T) {
	t.Setenv("SKIP_PULL", "1")

	ok, err := pullImage(t.Context(), nil, "image-name")

	require.NoError(t, err)
	require.False(t, ok)
}

func TestPullImage(t *testing.T) {
	dockerClient := &mockDockerClient{}
	withPullImage(dockerClient, nil, false)

	ok, err := pullImage(t.Context(), dockerClient, "image-name")

	require.NoError(t, err)
	require.True(t, ok)
}

func TestPullImageFail(t *testing.T) {
	dockerClient := &mockDockerClient{}
	withPullImage(dockerClient, errors.New("error pulling image"), false)

	ok, err := pullImage(t.Context(), dockerClient, "image-name")

	require.ErrorIs(t, err, errUpdateFailure)
	require.False(t, ok)
}

type mockDockerClient struct {
	client.APIClient
	t *testing.T

	errServiceUpdate    error
	assertServiceUpdate func(t *testing.T, service swarm.ServiceSpec)
	serviceVersion      uint64

	errServiceInspectWithRaw error
	updateStates             []swarm.UpdateState

	taskListCalls [][]swarm.Task

	errImagePull        error
	imagePullReadCloser io.ReadCloser

	expectedContainerID string

	containerWaitRespChannel <-chan container.WaitResponse
	containerWaitErrChannel  <-chan error
}

func (c *mockDockerClient) ServiceUpdate(ctx context.Context, serviceID string, version swarm.Version, service swarm.ServiceSpec, options types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error) {
	c.serviceVersion = version.Index + 1
	// Only assert the service update if an assertion function is provided and the service version is less than 3
	// If it's 3 or more, it means the service is going through a rollback, and the mock does not support that yet.
	if c.assertServiceUpdate != nil && c.serviceVersion < 3 {
		c.assertServiceUpdate(c.t, service)
	}

	return swarm.ServiceUpdateResponse{}, c.errServiceUpdate
}

func (c *mockDockerClient) TaskList(ctx context.Context, options types.TaskListOptions) ([]swarm.Task, error) {
	taskListCall := c.taskListCalls[0]
	if len(c.taskListCalls) > 1 {
		c.taskListCalls = c.taskListCalls[1:]
	}

	return taskListCall, nil
}

func (c *mockDockerClient) ServiceInspectWithRaw(ctx context.Context, serviceID string, options types.ServiceInspectOptions) (swarm.Service, []byte, error) {
	statusUpdate := c.updateStates[0]
	if len(c.updateStates) > 1 {
		c.updateStates = c.updateStates[1:]
	}
	replicas := uint64(1)
	// To simulate a rollback, we set the replicas to 0 when the service version is 3
	// 3 means the service has been updated twice (initial version 1 + 2 updates)
	// I admit this is a bit hacky, but it works for the purpose of the test
	if c.serviceVersion == 3 {
		replicas = 0
	}

	return swarm.Service{
		ID: serviceID,
		UpdateStatus: &swarm.UpdateStatus{
			State: statusUpdate,
		},
		Meta: swarm.Meta{
			Version: swarm.Version{Index: c.serviceVersion},
		},
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Image: "image-name",
				},
			},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{
					Replicas: &replicas,
				},
			},
		},
	}, nil, c.errServiceInspectWithRaw
}

func (c *mockDockerClient) ImagePull(ctx context.Context, ref string, options image.PullOptions) (io.ReadCloser, error) {
	return c.imagePullReadCloser, c.errImagePull
}

func (c *mockDockerClient) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	assert.Equal(c.t, c.expectedContainerID, containerID)

	return nil
}

func (c *mockDockerClient) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	assert.Equal(c.t, c.expectedContainerID, containerID)

	return nil
}
func (c *mockDockerClient) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
	expectedCMD := []string{"/portainer", "--force-rollback"}
	for i, cmd := range expectedCMD {
		assert.Equal(c.t, cmd, config.Cmd[i])
	}

	return container.CreateResponse{
		ID: c.expectedContainerID,
	}, nil
}

func (c *mockDockerClient) ContainerWait(ctx context.Context, container string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	return c.containerWaitRespChannel, c.containerWaitErrChannel
}

func withPullImage(mockClient *mockDockerClient, err error, upToDate bool) {
	output := `{"status":"Image is up to date for image-name"}`
	if upToDate {
		output = `{"status":"Image is up to date for image-name"}`
	}

	mockClient.imagePullReadCloser = io.NopCloser(strings.NewReader(output))
	mockClient.errImagePull = err
}

func setUpSwarmService() *swarm.Service {
	one := uint64(1)
	return &swarm.Service{
		ID: "swarm-id",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Image: "image-name",
				},
			},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{
					Replicas: &one,
				},
			},
		},
		Meta: swarm.Meta{
			Version: swarm.Version{Index: 1},
		},
	}
}
