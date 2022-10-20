package update

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

// UpdateScheduleIDLabel is the label used to store the update schedule ID
const UpdateScheduleIDLabel = "io.portainer.update.scheduleId"

var errAgentUpdateFailure = errors.New("agent update failure")

func Update(ctx context.Context, dockerCli *client.Client, oldContainerId string, imageName string, scheduleId string) error {
	log.Info().
		Str("containerId", oldContainerId).
		Str("image", imageName).
		Msg("Starting update process")

		// We look for the existing container to copy its configuration
	log.Debug().
		Str("containerId", oldContainerId).
		Msg("Looking for container")

	oldContainer, err := dockerCli.ContainerInspect(ctx, oldContainerId)
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

	imageUpToDate, err := pullImage(ctx, dockerCli, imageName)
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

	newContainerID, err := createContainer(ctx, dockerCli, imageName, scheduleId, tempAgentContainerName, oldContainer)
	if err != nil {
		log.Err(err).
			Msg("Unable to create container")

		return cleanupContainerAndError(ctx, dockerCli, oldContainerId, newContainerID)
	}

	err = startContainer(ctx, dockerCli, oldContainer.ID, newContainerID)
	if err != nil {
		log.Err(err).
			Msg("Unable to start container")

		return cleanupContainerAndError(ctx, dockerCli, oldContainerId, newContainerID)
	}

	healthy, err := monitorHealth(ctx, dockerCli, newContainerID)
	if err != nil {
		log.Err(err).
			Msg("Unable to monitor container health")
		return cleanupContainerAndError(ctx, dockerCli, oldContainerId, newContainerID)
	}

	if !healthy {
		return cleanupContainerAndError(ctx, dockerCli, oldContainerId, newContainerID)
	}

	log.Info().
		Msg("New container is healthy. The old  will be removed.")

	tryRemoveOldContainer(ctx, dockerCli, oldContainer.ID)

	// rename new container to old container name
	err = dockerCli.ContainerRename(ctx, newContainerID, oldContainerName)
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

func cleanupContainerAndError(ctx context.Context, dockerCli *client.Client, oldContainerId, newContainerID string) error {
	log.Debug().
		Msg("An error occurred during the update process - removing newly created container")

	// should restart old container
	err := dockerCli.ContainerStart(ctx, oldContainerId, types.ContainerStartOptions{})
	if err != nil {
		log.Err(err).
			Msg("Unable to restart container, please restart it manually")
	}

	if newContainerID != "" {
		err = dockerCli.ContainerRemove(ctx, newContainerID, types.ContainerRemoveOptions{Force: true})
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

func pullImage(ctx context.Context, dockerCli *client.Client, imageName string) (bool, error) {
	if os.Getenv("SKIP_PULL") != "" {
		return false, nil
	}

	log.Debug().
		Str("image", imageName).
		Msg("Pulling Docker image")

	reader, err := dockerCli.ImagePull(ctx, imageName, types.ImagePullOptions{})
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

func applyNetworks(ctx context.Context, dockerCli *client.Client, containerID string, networks []string) error {
	// We have to join all the networks one by one after container creation
	log.Debug().
		Str("containerId", containerID).
		Interface("networks", networks).
		Msg("Joining container to Docker networks")

	for _, networkName := range networks {
		err := dockerCli.NetworkConnect(ctx, networkName, containerID, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

func tryRemoveOldContainer(ctx context.Context, dockerCli *client.Client, oldContainerId string) {
	log.Debug().
		Str("containerId", oldContainerId).
		Msg("Removing old Portainer agent container")

	// remove old container
	err := dockerCli.ContainerRemove(ctx, oldContainerId, types.ContainerRemoveOptions{Force: true})
	if err != nil {
		log.Warn().Err(err).Msg("Unable to remove old Portainer agent container")
	}
}

func monitorHealth(ctx context.Context, dockerCli *client.Client, containerId string) (bool, error) {
	// We then wait for the new agent to be ready and monitor its health
	// This is done by inspecting the agent healthcheck status
	log.Debug().
		Str("containerId", containerId).
		Msg("Monitoring new container health")

	container, err := dockerCli.ContainerInspect(ctx, containerId)
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
		container, err = dockerCli.ContainerInspect(ctx, containerId)
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

func startContainer(ctx context.Context, dockerCli *client.Client, oldContainerID, newContainerID string) error {
	// We then start the new agent container
	log.Debug().
		Str("containerId", newContainerID).
		Msg("Starting new container")

	err := dockerCli.ContainerStop(ctx, oldContainerID, nil)
	if err != nil {
		return errors.WithMessage(err, "Unable to stop old Portainer agent container")
	}

	err = dockerCli.ContainerStart(ctx, newContainerID, types.ContainerStartOptions{})
	if err != nil {
		return errors.WithMessage(err, "Unable to start new Portainer agent container")
	}

	return nil
}

func createContainer(ctx context.Context, dockerCli *client.Client, imageName, updateScheduleId string, tempContainerName string, oldContainer types.ContainerJSON) (string, error) {
	log.Debug().
		Str("containerName", tempContainerName).
		Str("image", imageName).
		Msg("Creating new container")

	containerConfigCopy, networks, networkConfig := copyContainerConfig(imageName, updateScheduleId, oldContainer.Config, oldContainer.NetworkSettings.Networks)

	newAgentContainer, err := dockerCli.ContainerCreate(ctx,
		containerConfigCopy,
		oldContainer.HostConfig,
		networkConfig,
		nil,
		tempContainerName,
	)
	if err != nil {
		return "", errors.WithMessage(err, "Unable to create new Portainer agent container")
	}

	err = applyNetworks(ctx, dockerCli, newAgentContainer.ID, networks)
	if err != nil {
		return newAgentContainer.ID, errors.WithMessage(err, "Unable to join Portainer agent container to network")
	}

	return newAgentContainer.ID, nil
}
