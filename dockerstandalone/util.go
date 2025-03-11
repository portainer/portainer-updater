package dockerstandalone

import (
	"fmt"
	"strings"
)

// UpdateScheduleIDLabel is the label used to store the update schedule ID
const UpdateScheduleIDLabel = "io.portainer.update.scheduleId"

// UpdateEnv updates the environment variables of a container to include the update schedule ID
func UpdateEnv(env []string, scheduleId string) []string {
	foundIndex := -1
	for index, envVar := range env {
		if strings.HasPrefix(envVar, "UPDATE_ID=") {
			foundIndex = index
		}
	}

	scheduleEnv := fmt.Sprintf("UPDATE_ID=%s", scheduleId)
	if foundIndex != -1 {
		env[foundIndex] = scheduleEnv
	} else {
		env = append(env, scheduleEnv)
	}

	return env
}

// UpdateLabels updates the labels of a container to include the update schedule ID
func UpdateLabels(labels map[string]string, scheduleId string) map[string]string {
	if labels == nil {
		labels = make(map[string]string)
	}

	labels[UpdateScheduleIDLabel] = scheduleId

	return labels
}
