package dockerstandalone

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/portainer/portainer-updater/logs"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	pkgerrors "github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

var errUpdateFailure = errors.New("update failure")

type UpdateOptions struct {
	Agent               bool
	ExtendedHealthCheck bool
}

func Update(ctx context.Context, dockerCli *client.Client, oldContainerId string, imageName string, updateConfig func(*container.Config), options UpdateOptions) error {
	log.Info().
		Str("containerId", oldContainerId).
		Str("image", imageName).
		Msg("Starting update process")

	// We look for the existing container to copy its configuration
	log.Debug().Str("containerId", oldContainerId).Msg("Looking for container")

	oldContainer, err := dockerCli.ContainerInspect(ctx, oldContainerId)
	if err != nil {
		log.Error().
			Err(err).
			Str("containerId", oldContainerId).
			Msg("Unable to inspect container")

		return errUpdateFailure
	}

	log.Debug().
		Str("image", imageName).
		Str("containerImage", oldContainer.Config.Image).
		Msg("Checking whether the latest image is available")

	imageUpToDate, err := pullImage(ctx, dockerCli, imageName)
	if err != nil {
		log.Err(err).Msg("Unable to pull image")

		return errUpdateFailure
	}

	if oldContainer.Config.Image == imageName && imageUpToDate {
		log.Info().
			Str("image", imageName).
			Str("containerId", oldContainerId).
			Msg("Image is already up to date, shutting down")

		return nil
	}

	oldContainerName := strings.TrimPrefix(oldContainer.Name, "/")

	// We create the new container
	tempContainerName := buildContainerName(oldContainerName)

	newContainerID, err := createContainer(ctx, dockerCli, imageName, tempContainerName, oldContainer, updateConfig)
	if err != nil {
		log.Err(err).Msg("Unable to create container")

		return cleanupContainerAndError(ctx, dockerCli, oldContainerId, newContainerID, false)
	}

	log.Info().
		Str("containerId", oldContainerId).
		Str("image", imageName).
		Msg("Stopping old container")

	if err := dockerCli.ContainerStop(ctx, oldContainer.ID, container.StopOptions{}); err != nil {
		log.Err(err).Msg("Unable to stop container")

		return cleanupContainerAndError(ctx, dockerCli, oldContainerId, newContainerID, false)
	}

	log.Info().
		Str("containerId", newContainerID).
		Str("image", tempContainerName).
		Msg("Starting new container")

	// This flag indicates whether we should roll back database changes in case of failure
	// It is there to ensure backwards compatibility with previous versions of Portainer
	rollbackDB := options.ExtendedHealthCheck

	if err := dockerCli.ContainerStart(ctx, newContainerID, container.StartOptions{}); err != nil {
		log.Err(err).Msg("Unable to start container")

		return cleanupContainerAndError(ctx, dockerCli, oldContainerId, newContainerID, rollbackDB)
	}

	healthy, err := monitorHealth(ctx, dockerCli, newContainerID)
	if err != nil {
		log.Err(err).Msg("Unable to monitor container health")
		return cleanupContainerAndError(ctx, dockerCli, oldContainerId, newContainerID, rollbackDB)
	}

	if !healthy {
		return cleanupContainerAndError(ctx, dockerCli, oldContainerId, newContainerID, rollbackDB)
	}

	switch {
	case options.Agent:
		healthy, err = monitorAgentHealth(ctx, dockerCli, newContainerID, IsAsyncAgent(oldContainer))
	case options.ExtendedHealthCheck:
		healthy, err = monitorPortainerHealth(ctx, dockerCli, newContainerID)
	default:
		err = nil
		healthy = true
	}
	if err != nil {
		log.Err(err).
			Bool("ExtendedHealthCheck", options.ExtendedHealthCheck).
			Bool("Agent", options.Agent).
			Msg("Unable to monitor extended container health")

		return cleanupContainerAndError(ctx, dockerCli, oldContainerId, newContainerID, rollbackDB)
	}
	if !healthy {
		return cleanupContainerAndError(ctx, dockerCli, oldContainerId, newContainerID, rollbackDB)
	}

	log.Info().Msg("New container is healthy. The old  will be removed.")

	tryRemoveOldContainer(ctx, dockerCli, oldContainer.ID)

	// rename new container to old container name
	if err := dockerCli.ContainerRename(ctx, newContainerID, oldContainerName); err != nil {
		log.Err(err).Msg("Unable to rename container")

		return nil
	}

	log.Info().
		Str("containerId", newContainerID).
		Str("image", imageName).
		Str("containerName", oldContainerName).
		Msg("Update process completed")

	return nil
}

func cleanupContainerAndError(ctx context.Context, dockerCli client.APIClient, oldContainerId, newContainerID string, rollbackDB bool) error {
	log.Info().Msg("An error occurred during the update process - removing newly created container")

	printLogsToStdout(ctx, dockerCli, newContainerID)

	if rollbackDB {
		if err := execRollbackDB(ctx, dockerCli, oldContainerId, newContainerID, 5*time.Minute); err != nil {
			log.Err(err).Msg("Unable to rollback database changes. Manual rollback might be required")
		}
	}

	if err := dockerCli.ContainerRemove(ctx, newContainerID, container.RemoveOptions{Force: true}); err != nil {
		log.Err(err).Msg("Unable to remove temporary container, please remove it manually")
	}

	// should restart old container
	if err := dockerCli.ContainerStart(ctx, oldContainerId, container.StartOptions{}); err != nil {
		log.Err(err).
			Str("containerId", oldContainerId).
			Msg("Unable to restart container, please restart it manually")
	}

	log.Info().Msg("Successfully restarted old container and cleaned up temporary container")

	return errUpdateFailure
}

func buildContainerName(containerName string) string {
	if before, ok := strings.CutSuffix(containerName, "-update"); ok {
		return before
	}

	return containerName + "-update"
}

func pullImage(ctx context.Context, dockerCli *client.Client, imageName string) (bool, error) {
	if os.Getenv("SKIP_PULL") != "" {
		return false, nil
	}

	imagePullOptions, err := MakeImagePullOptions()
	if err != nil {
		return false, fmt.Errorf("unable to make image pull options: %w", err)
	}

	log.Debug().Str("image", imageName).Msg("Pulling Docker image")

	reader, err := dockerCli.ImagePull(ctx, imageName, imagePullOptions)
	if err != nil {
		log.Err(err).Str("image", imageName).Msg("Unable to pull image")

		return false, errUpdateFailure
	}
	defer logs.CloseAndLogErr(reader)

	// We have to read the output of the ImagePull command - otherwise it will be done asynchronously
	// This is not really well documented on the Docker SDK
	var imagePullOutputBuf bytes.Buffer
	tee := io.TeeReader(reader, &imagePullOutputBuf)

	if _, err := io.Copy(os.Stdout, tee); err != nil {
		return false, err
	}

	if _, err := io.Copy(&imagePullOutputBuf, reader); err != nil {
		return false, err
	}

	// TODO: REVIEW
	// There might be a cleaner way to check whether the container is using the same image as the one available locally
	// Maybe through image digest validation instead of checking the output of the docker pull command
	return strings.Contains(imagePullOutputBuf.String(), "Image is up to date"), nil
}

func copyContainerConfig(imageName string, config *container.Config, containerNetworks map[string]*network.EndpointSettings) (newConfig *container.Config, networks []string, networkConfig *network.NetworkingConfig) {
	// We copy the original Portainer configuration and apply a few changes:
	// * we replace the image name
	// * we strip the hostname from the original configuration to avoid networking issues with the internal Docker DNS
	// * we remove the original container healthcheck as we should use the one embedded in the target version image
	containerConfigCopy := config
	containerConfigCopy.Image = imageName
	containerConfigCopy.Hostname = ""
	containerConfigCopy.Healthcheck = nil

	// We add the new container in the same Docker container networks as the previous container
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

func applyNetworks(ctx context.Context, dockerCli client.APIClient, containerID string, networks []string) error {
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
		Msg("Removing old container")

	// remove old container
	err := dockerCli.ContainerRemove(ctx, oldContainerId, container.RemoveOptions{Force: true})
	if err != nil {
		log.Warn().Err(err).Msg("Unable to remove old container")
	}
}

func monitorAgentHealth(ctx context.Context, dockerCli *client.Client, containerID string, asyncMode bool) (bool, error) {
	log.Info().
		Str("containerId", containerID).
		Msg("Monitoring new agent container health by checking for the health file")
	backoffBase := 5
	if asyncMode {
		backoffBase = 60
	}
	retries := 10

	return monitorExtendedHealth(ctx, dockerCli, containerID, agentHealthy, backoffBase, "Agent", retries)
}

func monitorPortainerHealth(ctx context.Context, dockerCli *client.Client, containerID string) (bool, error) {
	log.Info().
		Str("containerId", containerID).
		Msg("Monitoring new portainer container health by using its health check flag")
	backoffBase := 10
	retries := 44
	// Each iteration takes approximately 10 seconds.
	// With backoffBase 10s and the sleep time is backoffBase*i, the total time is:
	// Total wait time = Σ(backoffBase*i) for i = 1..44 = 9900 seconds ≈ 2.75 hours
	// Covers long startup tasks like DB migrations.
	return monitorExtendedHealth(ctx, dockerCli, containerID, portainerHealthy, backoffBase, "Portainer", retries)
}

func monitorExtendedHealth(ctx context.Context, dockerCli *client.Client, containerID string, healthCheck healthCheck, backoffBase int, name string, retries int) (bool, error) {
	var err error
	for i := range retries {
		err = healthCheck(ctx, dockerCli, containerID)
		if err == nil {
			log.Info().
				Str("containerId", containerID).
				Msgf("%s health check passed. The server is healthy.", name)
			return true, nil
		}
		if errors.Is(err, ErrBinaryNotFound) || errors.Is(err, ErrFlagNotSupported) {
			log.Warn().
				Err(err).
				Str("containerId", containerID).
				Msgf("%s health cannot be checked. Assuming health check passed.", name)

			return true, nil
		}

		backoff := backoffBase * i
		log.Info().
			Str("containerId", containerID).
			Err(err).
			Int("backoff", backoff).
			Msgf("%s health check failed. Retrying after backoff", name)
		time.Sleep(time.Duration(backoff) * time.Second)
	}

	log.Error().
		Msgf("%s health check timed out. Exiting without updating the container", name)

	return false, pkgerrors.Wrapf(err, "%s health check timed out", name)
}

func monitorHealth(ctx context.Context, dockerCli client.APIClient, containerId string) (bool, error) {
	// We then wait for the new container to be ready and monitor its health
	// This is done by inspecting the container healthcheck status
	log.Info().
		Str("containerId", containerId).
		Msg("Monitoring new container health")

	// wait for healthcheck to be available or for the container to be stopped in case of error
	time.Sleep(15 * time.Second)

	container, err := dockerCli.ContainerInspect(ctx, containerId)
	if err != nil {
		return false, pkgerrors.WithMessage(err, "Unable to inspect new container")
	}

	if container.State.Health == nil {
		if container.State.Status == "exited" {
			return false, pkgerrors.New("Container exited unexpectedly")
		}

		log.Info().
			Str("containerId", containerId).
			Str("status", container.State.Status).
			Msg("No health check found for the container. Assuming health check passed.")

		return true, nil
	}

	tries := 5
	for range tries {
		if container.State.Health.Status == types.Healthy {
			return true, nil
		}

		if container.State.Health.Status == types.Unhealthy {
			log.Error().
				Str("Status", container.State.Health.Status).
				Interface("Logs", container.State.Health.Log).
				Msg("Health check failed. Exiting without updating the container")

			return false, nil
		}

		log.Debug().
			Str("containerId", containerId).
			Str("status", container.State.Health.Status).
			Msg("Container health check in progress")

		time.Sleep(5 * time.Second)
		container, err = dockerCli.ContainerInspect(ctx, containerId)
		if err != nil {
			return false, pkgerrors.WithMessage(err, "Unable to inspect new container")
		}
	}

	log.Error().
		Str("status", container.State.Health.Status).
		Interface("logs", container.State.Health.Log).
		Msg("Health check timed out. Exiting without updating the container")

	return false, nil

}

func createContainer(ctx context.Context, dockerCli client.APIClient, imageName, tempContainerName string, oldContainer types.ContainerJSON, updateConfig func(*container.Config)) (string, error) {
	log.Debug().
		Str("containerName", tempContainerName).
		Str("image", imageName).
		Msg("Creating new container")

	containerConfigCopy, networks, networkConfig := copyContainerConfig(imageName, oldContainer.Config, oldContainer.NetworkSettings.Networks)

	updateConfig(containerConfigCopy)

	newContainer, err := dockerCli.ContainerCreate(ctx,
		containerConfigCopy,
		oldContainer.HostConfig,
		networkConfig,
		nil,
		tempContainerName,
	)
	if err != nil {
		return "", pkgerrors.WithMessage(err, "Unable to create new container")
	}

	err = applyNetworks(ctx, dockerCli, newContainer.ID, networks)
	if err != nil {
		return "", pkgerrors.WithMessage(err, "Unable to join container to network")
	}

	return newContainer.ID, nil
}

func printLogsToStdout(ctx context.Context, dockerCli client.APIClient, containerID string) {
	log.Debug().
		Str("containerId", containerID).
		Msg("Printing container logs to stdout")

	reader, err := dockerCli.ContainerLogs(ctx, containerID, container.LogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		log.Error().Err(err).Msg("Unable to get container logs")
		return
	}

	defer logs.CloseAndLogErr(reader)

	if _, err := io.Copy(os.Stdout, reader); err != nil {
		log.Error().Err(err).Msg("Unable to print container logs")
	}
}

func execRollbackDB(ctx context.Context, dockerCli client.APIClient, oldContainerID, newContainerID string, timeout time.Duration) error {
	log.Info().Msg("Executing database rollback")
	if err := dockerCli.ContainerStop(ctx, newContainerID, container.StopOptions{}); err != nil {
		return fmt.Errorf("unable to stop container %s: %w", newContainerID, err)
	}

	containerJSON, err := dockerCli.ContainerInspect(ctx, oldContainerID)
	if err != nil {
		return fmt.Errorf("unable to inspect container %s: %w", oldContainerID, err)
	}

	rollbackContainerID, err := createContainer(ctx, dockerCli, containerJSON.Image, oldContainerID+"-rollback", containerJSON, func(config *container.Config) {
		config.Cmd = []string{"/portainer", "--force-rollback"}
		config.Entrypoint = []string{}
	})
	if err != nil {
		return fmt.Errorf("unable to create rollback container: %w", err)
	}

	if err := RollbackDB(ctx, dockerCli, rollbackContainerID, timeout); err != nil {
		return fmt.Errorf("unable to rollback database: %w", err)
	}

	log.Info().Msg("Database rollback completed successfully")

	return nil
}
