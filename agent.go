package main

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
	"github.com/pkg/errors"
)

type AgentUpdateCommand struct {
	ContainerID string `arg:"" help:"ID or name of the agent container. e.g. portainer-agent or e9b3e57700ad" name:"container-id"`
	Image       string `arg:"" help:"Image of the agent to upgrade to. e.g. portainer/agent:latest" name:"image"`
	ScheduleId  string `arg:"" help:"Schedule ID of the agent to upgrade to. e.g. 1" name:"schedule-id"`
}

var errAgentUpdateFailure = errors.New("agent update failure")

func (r *AgentUpdateCommand) Run(cmdCtx *CommandExecutionContext) error {
	cmdCtx.logger.Infow("Starting Portainer agent upgrade process",
		"containerId", r.ContainerID,
		"image", r.Image,
	)

	// We look for the existing agent container to copy its configuration
	cmdCtx.logger.Debugw("Looking for Portainer agent container", "containerName", r.ContainerID)
	oldContainer, err := cmdCtx.dockerCLI.ContainerInspect(cmdCtx.context, r.ContainerID)
	if err != nil {
		cmdCtx.logger.Errorw("Unable to find Portainer agent container",
			"error", err,
		)
		return errAgentUpdateFailure
	}

	imageUpToDate, err := pullImage(cmdCtx, r.Image)
	if err != nil {
		cmdCtx.logger.Errorw("Unable to pull image", "error", err)
		return errAgentUpdateFailure
	}

	// We then check if the agent is running the latest version already
	cmdCtx.logger.Debugw("Checking whether the latest Portainer image is available",
		"image", r.Image,
		"containerImage", oldContainer.Config.Image,
	)

	if oldContainer.Config.Image == r.Image && imageUpToDate {
		cmdCtx.logger.Infow("Portainer agent already using the latest version of the image",
			"containerName", r.ContainerID,
			"image", r.Image,
		)

		return nil
	}

	oldContainerName := strings.TrimPrefix(oldContainer.Name, "/")

	// We create the new agent container
	tempAgentContainerName := buildAgentContainerName(oldContainerName)

	newContainerID, err := createContainer(cmdCtx, tempAgentContainerName, r.ScheduleId, r.Image, oldContainer)
	if err != nil {
		cmdCtx.logger.Errorw("Unable to create Portainer agent container", "error", err)
		return cleanupContainerAndError(cmdCtx, r.ContainerID, newContainerID)
	}

	err = startContainer(cmdCtx, oldContainer.ID, newContainerID)
	if err != nil {
		cmdCtx.logger.Errorw("Unable to start Portainer agent container", "error", err)
		return cleanupContainerAndError(cmdCtx, r.ContainerID, newContainerID)
	}

	healthy, err := monitorHealth(cmdCtx, newContainerID)
	if err != nil {
		cmdCtx.logger.Errorw("Unable to monitor new Portainer agent container health", "error", err)
		return cleanupContainerAndError(cmdCtx, r.ContainerID, newContainerID)
	}

	if !healthy {
		return cleanupContainerAndError(cmdCtx, r.ContainerID, newContainerID)
	}

	cmdCtx.logger.Info("New Portainer agent container is healthy. The old Portainer agent container will be removed.")

	removeOldContainer(cmdCtx, oldContainer.ID)

	// rename new container to old container name
	err = cmdCtx.dockerCLI.ContainerRename(cmdCtx.context, newContainerID, oldContainerName)
	if err != nil {
		cmdCtx.logger.Errorw("Unable to rename new Portainer agent container", "error", err)
		return nil
	}

	cmdCtx.logger.Infow("Portainer agent upgrade process completed",
		"containerName", oldContainerName,
		"containerID", newContainerID,
		"image", r.Image,
	)

	return nil
}

func cleanupContainerAndError(cmdCtx *CommandExecutionContext, oldContainerId, newContainerID string) error {
	cmdCtx.logger.Debugw("An error occurred during the upgrade process - removing newly created Portainer agent container",
		"containerID", newContainerID,
	)

	// should restart old container
	err := cmdCtx.dockerCLI.ContainerStart(cmdCtx.context, oldContainerId, types.ContainerStartOptions{})
	if err != nil {
		cmdCtx.logger.Errorw("Unable to restart old Portainer agent container", "error", err)
	}

	if newContainerID != "" {
		err = cmdCtx.dockerCLI.ContainerRemove(cmdCtx.context, newContainerID, types.ContainerRemoveOptions{Force: true})
		if err != nil {
			cmdCtx.logger.Errorw("Unable to remove new Portainer agent container", "error", err)
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

func pullImage(cmdCtx *CommandExecutionContext, imageName string) (bool, error) {
	if os.Getenv("SKIP_PULL") != "" {
		return false, nil
	}

	cmdCtx.logger.Debugw("Pulling Docker image", "image", imageName)

	reader, err := cmdCtx.dockerCLI.ImagePull(cmdCtx.context, imageName, types.ImagePullOptions{})
	if err != nil {
		cmdCtx.logger.Errorw("Unable to pull Docker image", "error", err)
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
		if strings.HasPrefix(env, "UPDATE_SCHEDULE_ID=") {
			foundIndex = index
		}
	}

	scheduleEnv := fmt.Sprintf("UPDATE_SCHEDULE_ID=%s", updateScheduleId)
	if foundIndex != -1 {
		containerConfigCopy.Env[foundIndex] = scheduleEnv
	} else {
		containerConfigCopy.Env = append(containerConfigCopy.Env, scheduleEnv)
	}

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

func applyNetworks(context context.Context, cmdCtx *CommandExecutionContext, containerID string, networks []string) error {
	// We have to join all the networks one by one after container creation
	cmdCtx.logger.Debugw("Joining Portainer agent container to Docker networks",
		"networks", networks,
		"containerID", containerID,
	)

	for _, networkName := range networks {
		err := cmdCtx.dockerCLI.NetworkConnect(context, networkName, containerID, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

func removeOldContainer(cmdCtx *CommandExecutionContext, oldContainerId string) error {
	cmdCtx.logger.Debugw("Removing old Portainer agent container",
		"containerID", oldContainerId,
	)

	// remove old container
	err := cmdCtx.dockerCLI.ContainerRemove(cmdCtx.context, oldContainerId, types.ContainerRemoveOptions{Force: true})
	if err != nil {
		cmdCtx.logger.Warnw("Unable to remove old Portainer agent container", "error", err)
		// I think we shouldn't clean here. The new container is already running, so we should let the user decide what to do.
	}

	return nil
}

func monitorHealth(cmdCtx *CommandExecutionContext, containerId string) (bool, error) {
	// We then wait for the new agent to be ready and monitor its health
	// This is done by inspecting the agent healthcheck status
	cmdCtx.logger.Debug("Monitoring new Portainer agent container health")

	container, err := cmdCtx.dockerCLI.ContainerInspect(cmdCtx.context, containerId)
	if err != nil {
		return false, errors.WithMessage(err, "Unable to inspect new Portainer agent container")
	}

	if container.State.Health == nil {
		cmdCtx.logger.Info("No health check found for the new Portainer agent container. Assuming health check passed.")
		return true, nil
	}

	// TODO: REVIEW
	// The agent should either have a successful health check or the health check timeout would have kicked in after 15secs
	// Might be reviewed as well accordingly to the HEALTHCHECK instruction in the agent Dockerfile
	time.Sleep(15 * time.Second)

	if container.State.Health.Status != "healthy" {
		cmdCtx.logger.Errorw("New Portainer agent container health check failed. Exiting without updating the agent",
			"status", container.State.Health.Status,
			"logs", container.State.Health.Log,
		)
		return false, nil
	}

	return true, nil
}

func startContainer(cmdCtx *CommandExecutionContext, oldContainerID, newContainerID string) error {
	// We then start the new agent container
	cmdCtx.logger.Debugw("Starting new Portainer agent container", "containerID", newContainerID)

	err := cmdCtx.dockerCLI.ContainerStop(cmdCtx.context, oldContainerID, nil)
	if err != nil {
		return errors.WithMessage(err, "Unable to stop old Portainer agent container")
	}

	err = cmdCtx.dockerCLI.ContainerStart(cmdCtx.context, newContainerID, types.ContainerStartOptions{})
	if err != nil {
		return errors.WithMessage(err, "Unable to start new Portainer agent container")
	}

	return nil
}

func createContainer(cmdCtx *CommandExecutionContext, imageName, updateScheduleId string, tempContainerName string, oldContainer types.ContainerJSON) (string, error) {
	cmdCtx.logger.Debugw("Creating new Portainer agent container",
		"containerName", tempContainerName,
		"image", imageName,
	)

	containerConfigCopy, networks, networkConfig := copyContainerConfig(imageName, updateScheduleId, oldContainer.Config, oldContainer.NetworkSettings.Networks)

	newAgentContainer, err := cmdCtx.dockerCLI.ContainerCreate(cmdCtx.context,
		containerConfigCopy,
		oldContainer.HostConfig,
		networkConfig,
		nil,
		tempContainerName,
	)
	if err != nil {
		return "", errors.WithMessage(err, "Unable to create new Portainer agent container")
	}

	err = applyNetworks(cmdCtx.context, cmdCtx, newAgentContainer.ID, networks)
	if err != nil {
		return newAgentContainer.ID, errors.WithMessage(err, "Unable to join Portainer agent container to network")
	}

	return newAgentContainer.ID, nil
}
