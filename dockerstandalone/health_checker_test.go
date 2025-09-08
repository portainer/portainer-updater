package dockerstandalone

import (
	"testing"

	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
)

func TestAgentHealthChecker_healthyNoBinary(t *testing.T) {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer dockerCli.Close()

	response := setUpTestContainer(t, t.Context(), dockerCli)

	assert.ErrorIs(t, agentHealthy(t.Context(), dockerCli, response.ID), ErrBinaryNotFound, "should not contain healthy binary, thus should return ErrBinaryNotFound")
}

func TestPortainerHealthChecker_healthyNoFlag(t *testing.T) {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer dockerCli.Close()

	response := setUpTestContainer(t, t.Context(), dockerCli)

	assert.ErrorIs(t, portainerHealthy(t.Context(), dockerCli, response.ID), ErrFlagNotSupported, "should not contain a binary whose got a health check flag, thus should return ErrFlagNotSupported")
}
