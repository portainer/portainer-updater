package dockerstandalone

import (
	"encoding/base64"
	"testing"

	"github.com/docker/docker/api/types/registry"
	"github.com/segmentio/encoding/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMakeImagePullOptions_RegistryUsedSet(t *testing.T) {
	withCustomRegistryEnv(t, "user", "pass")

	opts, err := MakeImagePullOptions()

	require.NoError(t, err)
	assert.NotEmpty(t, opts.RegistryAuth)

	assertRegistryAuth(t, opts.RegistryAuth, "user", "pass")
}

func TestMakeImagePullOptions_RegistryUsedNotSet(t *testing.T) {
	t.Setenv("REGISTRY_USED", "")

	opts, err := MakeImagePullOptions()
	require.NoError(t, err)
	assert.Empty(t, opts.RegistryAuth)
}

func withCustomRegistryEnv(t *testing.T, username, password string) {
	t.Setenv("REGISTRY_USED", "1")
	t.Setenv("REGISTRY_USERNAME", username)
	t.Setenv("REGISTRY_PASSWORD", password)
}

func assertRegistryAuth(t *testing.T, base64registryAuth, expectedUsername, expectedPassword string) {
	decodedBytes, err := base64.URLEncoding.DecodeString(base64registryAuth)
	require.NoError(t, err)

	var registryAuth registry.AuthConfig
	require.NoError(t, json.Unmarshal(decodedBytes, &registryAuth))

	assert.Equal(t, expectedUsername, registryAuth.Username)
	assert.Equal(t, expectedPassword, registryAuth.Password)
}
