package main

import (
	"errors"
	"os"
	"testing"

	"github.com/portainer/portainer/api/filesystem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteTerminationMessage(t *testing.T) {
	t.Parallel()

	t.Run("writes the error when the file exists", func(t *testing.T) {
		t.Parallel()
		path := filesystem.JoinPaths(t.TempDir(), "termination-log")
		require.NoError(t, os.WriteFile(path, nil, 0o600))

		writeTerminationMessage(path, errors.New("nothing to update"))

		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "nothing to update", string(content))
	})

	t.Run("does nothing when the file is missing", func(t *testing.T) {
		t.Parallel()
		path := filesystem.JoinPaths(t.TempDir(), "termination-log")

		writeTerminationMessage(path, errors.New("nothing to update"))

		assert.NoFileExists(t, path)
	})
}
