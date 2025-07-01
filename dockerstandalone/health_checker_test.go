package dockerstandalone

import (
	"context"
	"testing"

	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
)

func TestHealthChecker_healthyNoBinary(t *testing.T) {
	ctx := context.Background()
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer dockerCli.Close()

	response := setUpAgentContainerWithoutHealthyBinary(t, ctx, dockerCli)

	assert.ErrorIs(t, defaultHealthChecker.healthy(ctx, dockerCli, response.ID), ErrBinaryNotFound, "should not contain healthy binary, thus should return ErrBinaryNotFound")
}
