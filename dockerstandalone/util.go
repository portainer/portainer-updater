package dockerstandalone

import (
	"encoding/base64"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/registry"
	"github.com/segmentio/encoding/json"
)

// UpdateScheduleIDLabel is the label used to store the update schedule ID
const UpdateScheduleIDLabel = "io.portainer.update.scheduleId"

// UpdateEnv updates the environment variables of a container to include the update schedule ID
func UpdateEnv(env []string, scheduleId string) []string {
	foundIndex := slices.IndexFunc(env, func(envVar string) bool {
		return strings.HasPrefix(envVar, "UPDATE_ID=")
	})

	scheduleEnv := "UPDATE_ID=" + scheduleId
	if foundIndex != -1 {
		env[foundIndex] = scheduleEnv

		return env
	}

	return append(env, scheduleEnv)
}

// UpdateLabels updates the labels of a container to include the update schedule ID
func UpdateLabels(labels map[string]string, scheduleId string) map[string]string {
	if labels == nil {
		labels = make(map[string]string)
	}

	labels[UpdateScheduleIDLabel] = scheduleId

	return labels
}

func IsAsyncAgent(container types.ContainerJSON) bool {
	if slices.Contains(container.Args, "--async-mode") {
		return true
	}

	const edgeAsyncEnv = "EDGE_ASYNC="
	for _, env := range container.Config.Env {
		if after, ok := strings.CutPrefix(env, edgeAsyncEnv); ok {
			value := after
			if value == "true" || value == "1" {
				return true
			}
		}
	}

	return false
}

func MakeImagePullOptions() (image.PullOptions, error) {
	var imagePullOptions image.PullOptions
	var err error
	if os.Getenv("REGISTRY_USED") != "" {
		imagePullOptions, err = CustomRegistryPullOptions()
		if err != nil {
			return image.PullOptions{}, fmt.Errorf("unable to make custom registry pull options: %w", err)
		}
	}

	return imagePullOptions, nil
}

func CustomRegistryPullOptions() (image.PullOptions, error) {
	var imagePullOptions image.PullOptions
	// Authenticate to the private registry
	// ref@https://docs.docker.com/engine/api/sdk/examples/#pull-an-image-with-authentication
	authConfig := registry.AuthConfig{
		Username: os.Getenv("REGISTRY_USERNAME"),
		Password: os.Getenv("REGISTRY_PASSWORD"),
	}

	encodedJSON, err := json.Marshal(authConfig)
	if err != nil {
		return image.PullOptions{}, err
	}

	imagePullOptions.RegistryAuth = base64.URLEncoding.EncodeToString(encodedJSON)

	return imagePullOptions, nil
}
