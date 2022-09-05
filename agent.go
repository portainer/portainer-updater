package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
)

type AgentUpdateCommand struct {
	ContainerID string `arg:"" help:"ID or name of the agent container. e.g. portainer-agent or e9b3e57700ad" name:"container-id"`
	Image       string `arg:"" help:"Image of the agent to upgrade to. e.g. portainer/agent:latest" name:"image" default:"portainer/agent:latest"`
}

var errAgentUpdateFailure = errors.New("agent update failure")

func (r *AgentUpdateCommand) Run(cmdCtx *CommandExecutionContext) error {
	cmdCtx.logger.Infow("Starting Portainer agent upgrade process",
		"containerId", r.ContainerID,
		"image", r.Image,
	)

	agentContainer, err := cmdCtx.dockerCLI.ContainerInspect(cmdCtx.context, r.ContainerID)
	if err != nil {
		cmdCtx.logger.Errorw("Unable to find Portainer agent container",
			"error", err,
		)
		return errAgentUpdateFailure
	}

	// First, we pull the new agent Docker image

	cmdCtx.logger.Debugw("Pulling Docker image", "image", r.Image)

	reader, err := cmdCtx.dockerCLI.ImagePull(cmdCtx.context, r.Image, types.ImagePullOptions{})
	if err != nil {
		cmdCtx.logger.Errorw("Unable to pull Docker image", "error", err)
		return errAgentUpdateFailure
	}
	defer reader.Close()

	// We have to read the output of the ImagePull command - otherwise it will be done asynchronously
	// This is not really well documented on the Docker SDK
	var imagePullOutputBuf bytes.Buffer
	tee := io.TeeReader(reader, &imagePullOutputBuf)

	io.Copy(os.Stdout, tee)
	io.Copy(&imagePullOutputBuf, reader)

	// We look for the existing agent container to copy its configuration
	cmdCtx.logger.Debugw("Looking for Portainer agent container",
		"containerName", r.ContainerID,
	)

	// We then check if the agent is running the latest version already
	cmdCtx.logger.Debugw("Checking whether the latest Portainer image is available",
		"image", r.Image,
		"containerImage", agentContainer.Config.Image,
	)

	// TODO: REVIEW
	// There might be a cleaner way to check whether the agent is using the same image as the one available locally
	// Maybe through image digest validation instead of checking the output of the docker pull command
	if agentContainer.Config.Image == r.Image && strings.Contains(imagePullOutputBuf.String(), "Image is up to date") {
		cmdCtx.logger.Infow("Portainer agent already using the latest version of the image",
			"containerName", r.ContainerID,
			"image", r.Image,
		)

		return nil
	}

	oldContainerName := strings.TrimPrefix(agentContainer.Name, "/")

	// We create the new agent container
	updatedAgentContainerName := buildAgentContainerName(oldContainerName)
	cmdCtx.logger.Debugw("Creating new Portainer agent container",
		"containerName", updatedAgentContainerName,
		"image", r.Image,
	)

	// We copy the original Portainer agent configuration and apply a few changes:
	// * we replace the image name
	// * we strip the hostname from the original configuration to avoid networking issues with the internal Docker DNS
	// * we remove the original agent container healthcheck as we should use the one embedded in the target version image
	containerConfigCopy := agentContainer.Config
	containerConfigCopy.Image = r.Image
	containerConfigCopy.Hostname = ""
	containerConfigCopy.Healthcheck = nil

	// We add the new agent in the same Docker container networks as the previous agent
	// This configuration is copied to the new container configuration
	containerEndpointsConfig := make(map[string]*network.EndpointSettings)
	networks := make([]string, 0)

	for networkName := range agentContainer.NetworkSettings.Networks {
		networks = append(networks, networkName)
		containerEndpointsConfig[networkName] = &network.EndpointSettings{}
	}

	newAgentContainer, err := cmdCtx.dockerCLI.ContainerCreate(cmdCtx.context,
		containerConfigCopy,
		agentContainer.HostConfig,
		&network.NetworkingConfig{
			EndpointsConfig: containerEndpointsConfig,
		}, nil, updatedAgentContainerName,
	)
	if err != nil {
		cmdCtx.logger.Errorw("Unable to create new Portainer agent container",
			"error", err,
		)
		return errAgentUpdateFailure
	}

	newAgentContainerID := newAgentContainer.ID

	// We have to join all the networks one by one after container creation
	cmdCtx.logger.Debugw("Joining Portainer agent container to Docker networks",
		"networks", networks,
		"containerID", newAgentContainerID,
	)

	for _, networkName := range networks {
		err := cmdCtx.dockerCLI.NetworkConnect(cmdCtx.context, networkName, newAgentContainerID, nil)
		if err != nil {
			cmdCtx.logger.Errorw("Unable to join Portainer agent container to network",
				"network", networkName,
				"error", err,
			)
			return cleanupContainerAndError(cmdCtx, r.ContainerID, newAgentContainerID)
		}
	}

	// We then start the new agent container
	cmdCtx.logger.Debugw("Starting new Portainer agent container",
		"containerName", updatedAgentContainerName,
		"containerID", newAgentContainerID,
	)

	err = cmdCtx.dockerCLI.ContainerStop(cmdCtx.context, r.ContainerID, nil)
	if err != nil {
		cmdCtx.logger.Errorw("Unable to stop old Portainer agent container", "error", err)
		return cleanupContainerAndError(cmdCtx, r.ContainerID, newAgentContainerID)
	}

	err = cmdCtx.dockerCLI.ContainerStart(cmdCtx.context, newAgentContainerID, types.ContainerStartOptions{})
	if err != nil {
		cmdCtx.logger.Errorw("Unable to start new Portainer agent container",
			"error", err,
		)
		return cleanupContainerAndError(cmdCtx, r.ContainerID, newAgentContainerID)
	}

	// We then wait for the new agent to be ready and monitor its health
	// This is done by inspecting the agent healthcheck status
	cmdCtx.logger.Debug("Monitoring new Portainer agent container health")

	newCntr, err := cmdCtx.dockerCLI.ContainerInspect(cmdCtx.context, newAgentContainerID)
	if err != nil {
		cmdCtx.logger.Errorw("Unable to inspect new Portainer agent container",
			"error", err,
		)
		return cleanupContainerAndError(cmdCtx, r.ContainerID, newAgentContainerID)
	}

	if newCntr.State.Health != nil {
		// TODO: REVIEW
		// The agent should either have a successful health check or the health check timeout would have kicked in after 15secs
		// Might be reviewed as well accordingly to the HEALTHCHECK instruction in the agent Dockerfile
		time.Sleep(15 * time.Second)

		if newCntr.State.Health.Status != "healthy" {
			cmdCtx.logger.Errorw("New Portainer agent container health check failed. Exiting without updating the agent",
				"status", newCntr.State.Health.Status,
				"logs", newCntr.State.Health.Log,
			)
			return cleanupContainerAndError(cmdCtx, r.ContainerID, newAgentContainerID)
		}

		cmdCtx.logger.Info("New Portainer agent container is healthy. The old Portainer agent container will be removed.")
	} else {
		cmdCtx.logger.Info("No health check found for the new Portainer agent container. Assuming health check passed.")
	}

	// We then remove the old agent container
	cmdCtx.logger.Debugw("Removing old Portainer agent container",
		"containerName", updatedAgentContainerName,
		"containerID", agentContainer.ID,
	)

	// remove old container
	err = cmdCtx.dockerCLI.ContainerRemove(cmdCtx.context, agentContainer.ID, types.ContainerRemoveOptions{Force: true})
	if err != nil {
		cmdCtx.logger.Warnw("Unable to remove old Portainer agent container", "error", err)
		// I think we shouldn't clean here. The new container is already running, so we should let the user decide what to do.
		return nil
	}

	// rename new container to old container name
	err = cmdCtx.dockerCLI.ContainerRename(cmdCtx.context, newAgentContainerID, oldContainerName)
	if err != nil {
		cmdCtx.logger.Errorw("Unable to rename new Portainer agent container", "error", err)
		return nil
	}

	cmdCtx.logger.Infow("Portainer agent upgrade process completed",
		"containerName", oldContainerName,
		"containerID", newAgentContainerID,
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

	err = cmdCtx.dockerCLI.ContainerRemove(cmdCtx.context, newContainerID, types.ContainerRemoveOptions{Force: true})
	if err != nil {
		cmdCtx.logger.Errorw("Unable to remove new Portainer agent container",
			"error", err,
		)
	}

	return errAgentUpdateFailure
}

func buildAgentContainerName(containerName string) string {
	if strings.HasSuffix(containerName, "-update") {
		return strings.TrimSuffix(containerName, "-update")
	}

	return fmt.Sprintf("%s-update", containerName)
}
