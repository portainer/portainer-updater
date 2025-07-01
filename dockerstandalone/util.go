package dockerstandalone

import (
	"fmt"
	"slices"
	"strings"

	"github.com/docker/docker/api/types"
)

// UpdateScheduleIDLabel is the label used to store the update schedule ID
const UpdateScheduleIDLabel = "io.portainer.update.scheduleId"

// UpdateEnv updates the environment variables of a container to include the update schedule ID
func UpdateEnv(env []string, scheduleId string) []string {
	foundIndex := slices.IndexFunc(env, func(envVar string) bool {
		return strings.HasPrefix(envVar, "UPDATE_ID=")
	})

	scheduleEnv := fmt.Sprintf("UPDATE_ID=%s", scheduleId)
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
	for _, arg := range container.Args {
		if arg == "--async-mode" {
			return true
		}
	}

	const edgeAsyncEnv = "EDGE_ASYNC="
	for _, env := range container.Config.Env {
		if strings.HasPrefix(env, edgeAsyncEnv) {
			value := strings.TrimPrefix(env, edgeAsyncEnv)
			if value == "true" || value == "1" {
				return true
			}
		}
	}

	return false
}
