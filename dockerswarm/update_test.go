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
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
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

	swarmService := setUpSwarmService()
	imageName := "image-name"

	defaultAssertServiceUpdate := func(t *testing.T, service swarm.ServiceSpec) {
		require.Equal(t, imageName, service.TaskTemplate.ContainerSpec.Image)
		require.NotNil(t, service.TaskTemplate.ContainerSpec.Healthcheck)
		require.Equal(t, []string{"CMD", "/portainer", "--health-check"}, service.TaskTemplate.ContainerSpec.Healthcheck.Test)
	}

	t.Run("successful update", func(t *testing.T) {
		t.Cleanup(func() {
			swarmService.Version.Index = 1
		})

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
		var updateStatuses []swarm.UpdateState
		for range 10 {
			updateStatuses = append(updateStatuses, swarm.UpdateStateUpdating)
		}
		for range 5 {
			updateStatuses = append(updateStatuses, swarm.UpdateStateRollbackStarted)
		}
		updateStatuses = append(updateStatuses, swarm.UpdateStateRollbackCompleted)

		dockerClient := &mockDockerClient{
			t:                   t,
			assertServiceUpdate: defaultAssertServiceUpdate,
			errServiceUpdate:    nil,
			updateStates:        updateStatuses,
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

	errServiceInspectWithRaw error
	updateStates             []swarm.UpdateState

	errImagePull        error
	imagePullReadCloser io.ReadCloser
}

func (m *mockDockerClient) ServiceUpdate(ctx context.Context, serviceID string, version swarm.Version, service swarm.ServiceSpec, options types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error) {
	if m.assertServiceUpdate != nil {
		m.assertServiceUpdate(m.t, service)
	}

	return swarm.ServiceUpdateResponse{}, m.errServiceUpdate
}

func (m *mockDockerClient) ServiceInspectWithRaw(ctx context.Context, serviceID string, options types.ServiceInspectOptions) (swarm.Service, []byte, error) {
	statusUpdate := m.updateStates[0]
	if len(m.updateStates) > 1 {
		m.updateStates = m.updateStates[1:]
	}

	return swarm.Service{
		ID: serviceID,
		UpdateStatus: &swarm.UpdateStatus{
			State: statusUpdate,
		},
	}, nil, m.errServiceInspectWithRaw
}

func (m *mockDockerClient) ImagePull(ctx context.Context, ref string, options image.PullOptions) (io.ReadCloser, error) {
	return m.imagePullReadCloser, m.errImagePull
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
	return &swarm.Service{
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
}
