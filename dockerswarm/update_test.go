package dockerswarm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/require"
)

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

	err = Update(context.Background(), dockerCli, "image-name", swarmService, func(*swarm.ContainerSpec) {})
	require.Error(t, err)
	require.Equal(t, swarmService.Version.Index, uint64(2))
}
