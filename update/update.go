package update

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/pkg/errors"
	"github.com/portainer/portainer-updater/context"
	"github.com/rs/zerolog/log"
)

// UpdateScheduleIDLabel is the label used to store the update schedule ID
const UpdateScheduleIDLabel = "io.portainer.update.scheduleId"

var errAgentUpdateFailure = errors.New("agent update failure")

func Update(oldContainerId string, imageName string, scheduleId string, cmdCtx *context.CommandExecutionContext) error {
	log.Info().
		Str("containerId", oldContainerId).
		Str("image", imageName).
		Msg("Starting update process")

		// We look for the existing container to copy its configuration
	log.Debug().
		Str("containerId", oldContainerId).
		Msg("Looking for container")

	oldContainer, err := cmdCtx.DockerCLI.ContainerInspect(cmdCtx.Context, oldContainerId)
	if err != nil {
		log.Error().
			Err(err).
			Str("containerId", oldContainerId).
			Msg("Unable to inspect container")

		return errAgentUpdateFailure
	}

	log.Debug().
		Str("image", imageName).
		Str("containerImage", oldContainer.Config.Image).
		Msg("Checking whether the latest image is available")

	imageUpToDate, err := pullImage(cmdCtx, imageName)
	if err != nil {
		log.Err(err).
			Msg("Unable to pull image")

		return errAgentUpdateFailure
	}

	if oldContainer.Config.Image == imageName && imageUpToDate {
		log.Info().
			Str("image", imageName).
			Str("containerId", oldContainerId).
			Msg("Image is already up to date, shutting down")

		return nil
	}

	oldContainerName := strings.TrimPrefix(oldContainer.Name, "/")

	// We create the new agent container
	tempAgentContainerName := buildAgentContainerName(oldContainerName)

	newContainerID, err := createContainer(cmdCtx, imageName, scheduleId, tempAgentContainerName, oldContainer)
	if err != nil {
		log.Err(err).
			Msg("Unable to create container")

		return cleanupContainerAndError(cmdCtx, oldContainerId, newContainerID)
	}

	err = startContainer(cmdCtx, oldContainer.ID, newContainerID)
	if err != nil {
		log.Err(err).
			Msg("Unable to start container")

		return cleanupContainerAndError(cmdCtx, oldContainerId, newContainerID)
	}

	healthy, err := monitorHealth(cmdCtx, newContainerID)
	if err != nil {
		log.Err(err).
			Msg("Unable to monitor container health")
		return cleanupContainerAndError(cmdCtx, oldContainerId, newContainerID)
	}

	if !healthy {
		return cleanupContainerAndError(cmdCtx, oldContainerId, newContainerID)
	}

	log.Info().
		Msg("New container is healthy. The old  will be removed.")

	tryRemoveOldContainer(cmdCtx, oldContainer.ID)

	// rename new container to old container name
	err = cmdCtx.DockerCLI.ContainerRename(cmdCtx.Context, newContainerID, oldContainerName)
	if err != nil {
		log.Err(err).
			Msg("Unable to rename container")
		return nil
	}

	log.Info().
		Str("containerId", newContainerID).
		Str("image", imageName).
		Str("containerName", oldContainerName).
		Msg("Update process completed")

	return nil
}

func cleanupContainerAndError(cmdCtx *context.CommandExecutionContext, oldContainerId, newContainerID string) error {
	log.Debug().
		Msg("An error occurred during the update process - removing newly created container")

	// should restart old container
	err := cmdCtx.DockerCLI.ContainerStart(cmdCtx.Context, oldContainerId, types.ContainerStartOptions{})
	if err != nil {
		log.Err(err).
			Msg("Unable to restart container, please restart it manually")
	}

	if newContainerID != "" {
		err = cmdCtx.DockerCLI.ContainerRemove(cmdCtx.Context, newContainerID, types.ContainerRemoveOptions{Force: true})
		if err != nil {
			log.Err(err).
				Msg("Unable to remove temporary container, please remove it manually")
		}
	}

	return errAgentUpdateFailure
}

func buildAgentContainerName(containerName string) string {
	if strings.HasSuffix(containerName, "-update") {
		return strings.TrimSuffix(containerName, "-update")
	}

	return fmt.Sprintf("%s-update", containerName)
}

func pullImage(cmdCtx *context.CommandExecutionContext, imageName string) (bool, error) {
	if os.Getenv("SKIP_PULL") != "" {
		return false, nil
	}

	log.Debug().
		Str("image", imageName).
		Msg("Pulling Docker image")

	reader, err := cmdCtx.DockerCLI.ImagePull(cmdCtx.Context, imageName, types.ImagePullOptions{})
	if err != nil {
		log.Err(err).
			Str("image", imageName).
			Msg("Unable to pull image")

		return false, errAgentUpdateFailure
	}
	defer reader.Close()

	// We have to read the output of the ImagePull command - otherwise it will be done asynchronously
	// This is not really well documented on the Docker SDK
	var imagePullOutputBuf bytes.Buffer
	tee := io.TeeReader(reader, &imagePullOutputBuf)

	io.Copy(os.Stdout, tee)
	io.Copy(&imagePullOutputBuf, reader)

	// TODO: REVIEW
	// There might be a cleaner way to check whether the agent is using the same image as the one available locally
	// Maybe through image digest validation instead of checking the output of the docker pull command
	return strings.Contains(imagePullOutputBuf.String(), "Image is up to date"), nil
}

func copyContainerConfig(imageName string, updateScheduleId string, config *container.Config, containerNetworks map[string]*network.EndpointSettings) (newConfig *container.Config, networks []string, networkConfig *network.NetworkingConfig) {
	// We copy the original Portainer agent configuration and apply a few changes:
	// * we replace the image name
	// * we strip the hostname from the original configuration to avoid networking issues with the internal Docker DNS
	// * we remove the original agent container healthcheck as we should use the one embedded in the target version image
	containerConfigCopy := config
	containerConfigCopy.Image = imageName
	containerConfigCopy.Hostname = ""
	containerConfigCopy.Healthcheck = nil
	foundIndex := -1
	for index, env := range containerConfigCopy.Env {
		if strings.HasPrefix(env, "UPDATE_ID=") {
			foundIndex = index
		}
	}

	scheduleEnv := fmt.Sprintf("UPDATE_ID=%s", updateScheduleId)
	if foundIndex != -1 {
		containerConfigCopy.Env[foundIndex] = scheduleEnv
	} else {
		containerConfigCopy.Env = append(containerConfigCopy.Env, scheduleEnv)
	}

	if containerConfigCopy.Labels == nil {
		containerConfigCopy.Labels = make(map[string]string)
	}

	containerConfigCopy.Labels[UpdateScheduleIDLabel] = updateScheduleId

	// We add the new agent in the same Docker container networks as the previous agent
	// This configuration is copied to the new container configuration
	containerEndpointsConfig := make(map[string]*network.EndpointSettings)

	for networkName := range containerNetworks {
		networks = append(networks, networkName)
		containerEndpointsConfig[networkName] = &network.EndpointSettings{}
	}

	return containerConfigCopy, networks, &network.NetworkingConfig{
		EndpointsConfig: containerEndpointsConfig,
	}
}

func applyNetworks(cmdCtx *context.CommandExecutionContext, containerID string, networks []string) error {
	// We have to join all the networks one by one after container creation
	log.Debug().
		Str("containerId", containerID).
		Interface("networks", networks).
		Msg("Joining container to Docker networks")

	for _, networkName := range networks {
		err := cmdCtx.DockerCLI.NetworkConnect(cmdCtx.Context, networkName, containerID, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

func tryRemoveOldContainer(cmdCtx *context.CommandExecutionContext, oldContainerId string) {
	log.Debug().
		Str("containerId", oldContainerId).
		Msg("Removing old Portainer agent container")

	// remove old container
	err := cmdCtx.DockerCLI.ContainerRemove(cmdCtx.Context, oldContainerId, types.ContainerRemoveOptions{Force: true})
	if err != nil {
		log.Warn().Err(err).Msg("Unable to remove old Portainer agent container")
	}
}

func monitorHealth(cmdCtx *context.CommandExecutionContext, containerId string) (bool, error) {
	// We then wait for the new agent to be ready and monitor its health
	// This is done by inspecting the agent healthcheck status
	log.Debug().
		Str("containerId", containerId).
		Msg("Monitoring new container health")

	container, err := cmdCtx.DockerCLI.ContainerInspect(cmdCtx.Context, containerId)
	if err != nil {
		return false, errors.WithMessage(err, "Unable to inspect new Portainer agent container")
	}

	if container.State.Health == nil {
		log.Info().
			Str("containerId", containerId).
			Msg("No health check found for the container. Assuming health check passed.")

		return true, nil
	}

	time.Sleep(15 * time.Second)

	tries := 5
	for i := 0; i < tries; i++ {

		if container.State.Health.Status == "healthy" {
			return true, nil
		}

		if container.State.Health.Status == "unhealthy" {
			log.Error().
				Str("Status", container.State.Health.Status).
				Interface("Logs", container.State.Health.Log).
				Msg("Health check failed. Exiting without updating the agent")

			return false, nil
		}

		log.Debug().
			Str("containerId", containerId).
			Str("status", container.State.Health.Status).
			Msg("Container health check in progress")

		time.Sleep(5 * time.Second)
		container, err = cmdCtx.DockerCLI.ContainerInspect(cmdCtx.Context, containerId)
		if err != nil {
			return false, errors.WithMessage(err, "Unable to inspect new Portainer agent container")
		}
	}

	// cmdCtx.Logger.Errorw("New Portainer agent container health check timed out. Exiting without updating the agent",
	// 	"status", container.State.Health.Status,
	// 	"logs", container.State.Health.Log,
	// )
	log.Error().
		Str("status", container.State.Health.Status).
		Interface("logs", container.State.Health.Log).
		Msg("Health check timed out. Exiting without updating the agent")

	return false, nil

}

func startContainer(cmdCtx *context.CommandExecutionContext, oldContainerID, newContainerID string) error {
	// We then start the new agent container
	log.Debug().
		Str("containerId", newContainerID).
		Msg("Starting new container")

	err := cmdCtx.DockerCLI.ContainerStop(cmdCtx.Context, oldContainerID, nil)
	if err != nil {
		return errors.WithMessage(err, "Unable to stop old Portainer agent container")
	}

	err = cmdCtx.DockerCLI.ContainerStart(cmdCtx.Context, newContainerID, types.ContainerStartOptions{})
	if err != nil {
		return errors.WithMessage(err, "Unable to start new Portainer agent container")
	}

	return nil
}

func createContainer(cmdCtx *context.CommandExecutionContext, imageName, updateScheduleId string, tempContainerName string, oldContainer types.ContainerJSON) (string, error) {
	log.Debug().
		Str("containerName", tempContainerName).
		Str("image", imageName).
		Msg("Creating new container")

	containerConfigCopy, networks, networkConfig := copyContainerConfig(imageName, updateScheduleId, oldContainer.Config, oldContainer.NetworkSettings.Networks)

	newAgentContainer, err := cmdCtx.DockerCLI.ContainerCreate(cmdCtx.Context,
		containerConfigCopy,
		oldContainer.HostConfig,
		networkConfig,
		nil,
		tempContainerName,
	)
	if err != nil {
		return "", errors.WithMessage(err, "Unable to create new Portainer agent container")
	}

	err = applyNetworks(cmdCtx, newAgentContainer.ID, networks)
	if err != nil {
		return newAgentContainer.ID, errors.WithMessage(err, "Unable to join Portainer agent container to network")
	}

	return newAgentContainer.ID, nil
}
