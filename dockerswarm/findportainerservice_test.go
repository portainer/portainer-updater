package dockerswarm

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types/swarm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindPortainerService_Success(t *testing.T) {
	t.Parallel()
	updaterService := setUpService("portainer_updater", []string{"node.role == manager"})
	updaterService.Spec.Labels = make(map[string]string)
	updaterService.Spec.Labels["io.portainer.updater"] = "true"
	updaterService.Spec.TaskTemplate.ContainerSpec.Labels = updaterService.Spec.Labels
	portainerService := setUpService("portainer", []string{"node.role == manager"})
	agentService := setUpService("portainer_agent", []string{"node.platform.os == linux"})
	client := &mockDockerClient{serviceList: []swarm.Service{
		updaterService,
		agentService,
		portainerService,
	}}

	service, err := FindPortainerService(t.Context(), client)
	require.NoError(t, err)
	require.NotNil(t, service)

	assert.Equal(t, portainerService.Spec.Name, service.Spec.Name)
	assert.Equal(t, portainerService.ID, service.ID)
}

func TestFindPortainerService_NoServices(t *testing.T) {
	t.Parallel()
	client := &mockDockerClient{serviceList: []swarm.Service{}}

	service, err := FindPortainerService(t.Context(), client)

	require.Nil(t, service)
	require.ErrorContains(t, err, "no services found")
}

func TestFindPortainerService_NoManagerConstraint(t *testing.T) {
	t.Parallel()
	portainerService := setUpService("portainer", []string{})
	client := &mockDockerClient{serviceList: []swarm.Service{
		portainerService,
	}}

	service, err := FindPortainerService(t.Context(), client)

	require.Nil(t, service)
	require.ErrorContains(t, err, "no portainer service found")
}

func TestFindPortainerService_ServiceListError(t *testing.T) {
	t.Parallel()
	expectedError := errors.New("service list error")
	client := &mockDockerClient{
		errServiceList: expectedError,
	}

	service, err := FindPortainerService(context.Background(), client)

	require.Nil(t, service)
	require.ErrorIs(t, err, expectedError)
}

func (c *mockDockerClient) ServiceList(ctx context.Context, opts swarm.ServiceListOptions) ([]swarm.Service, error) {
	return c.serviceList, c.errServiceList
}

func setUpService(name string, constraints []string) swarm.Service {
	return swarm.Service{
		ID: "test-id-" + name,
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name: name,
			},
			TaskTemplate: swarm.TaskSpec{
				Placement: &swarm.Placement{
					Constraints: constraints,
				},
				ContainerSpec: &swarm.ContainerSpec{
					Labels: make(map[string]string),
				},
			},
		},
	}
}
