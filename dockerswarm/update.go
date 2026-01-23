package dockerswarm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/portainer/portainer-updater/dockerstandalone"
	"github.com/portainer/portainer-updater/logs"
	"github.com/portainer/portainer-updater/utils"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

type UpdateOptions struct {
	ExtendedHealthCheck bool
}

var errUpdateFailure = errors.New("update failure")

func Update(ctx context.Context, dockerCli client.APIClient, imageName string, service *swarm.Service, updateConfig func(*swarm.ContainerSpec), options UpdateOptions) error {
	log.Info().
		Str("serviceId", service.ID).
		Str("image", imageName).
		Msg("Starting update process")

	log.Debug().
		Str("image", imageName).
		Str("containerImage", service.Spec.TaskTemplate.ContainerSpec.Image).
		Msg("Checking whether the latest image is available")

	imageUpToDate, err := pullImage(ctx, dockerCli, imageName)
	if err != nil {
		log.Err(err).
			Msg("Unable to pull image")

		return errUpdateFailure
	}

	if service.Spec.TaskTemplate.ContainerSpec.Image == imageName && imageUpToDate {
		log.Info().
			Str("image", imageName).
			Str("serviceId", service.ID).
			Msg("Image is already up to date, shutting down")

		return nil
	}

	originalImage := service.Spec.TaskTemplate.ContainerSpec.Image
	service.Spec.TaskTemplate.ContainerSpec.Image = imageName

	updateConfig(service.Spec.TaskTemplate.ContainerSpec)
	prevVersion := service.Version
	service.Version = swarm.Version{Index: service.Version.Index + 1}

	service.Spec.UpdateConfig = &swarm.UpdateConfig{
		FailureAction: swarm.UpdateFailureActionRollback,
		Order:         swarm.UpdateOrderStopFirst,
	}

	awaitCompletion := 5 * time.Minute

	if options.ExtendedHealthCheck {
		service.Spec.TaskTemplate.ContainerSpec.Healthcheck = &container.HealthConfig{
			Test:     []string{"CMD", "/portainer", "--health-check"},
			Interval: 5 * time.Second,
			// The healthcheck is expected to complete within 5 seconds.
			// If it takes longer, Portainer is likely under high load or simply not responding.
			Timeout:       5 * time.Second,
			StartPeriod:   0,
			StartInterval: 0,
			// Infinite retries, is not supported, so we have to pick a reasonable number.
			// Each retry takes in the best case a few milliseconds and worst case 5 seconds, and is run every 5 seconds.
			// Because migrations can take a long time, we want to allow it to run for 2 hours.
			// So 2 hours / 5 seconds = 1440 retries, for the quickest possible retry.
			// And 2 hours / 10 seconds = 720 retries, for the slowest possible retry.
			// It's unlikely that the healthcheck will take the full 5 seconds every time, so 1000 retries should be sufficient.
			// Thus, the maximum time the health check can take is 1000 * 10 seconds = 10000 seconds = ~2.78 hours.
			Retries: 1000,
		}
		// We add some time so that the rollback has a chance to complete as well, so that the updater
		// can report the failure and finish cleanly.
		awaitCompletion = 3 * time.Hour
	} else if isPortainerHealthCheck(service.Spec.TaskTemplate.ContainerSpec.Healthcheck) {
		// This case is to reset the healthcheck to nil if it was previously set by Portainer Auto Update.
		// Doing so, will ensure that a user can roll back to a previous version of Portainer without the healthcheck
		service.Spec.TaskTemplate.ContainerSpec.Healthcheck = nil
	}

	updateResponse, err := dockerCli.ServiceUpdate(ctx, service.ID, prevVersion, service.Spec, types.ServiceUpdateOptions{})
	if err != nil {
		return errors.WithMessage(err, "unable to update service")
	}

	if len(updateResponse.Warnings) > 0 {
		log.Warn().
			Str("serviceId", service.ID).
			Interface("warnings", updateResponse.Warnings).
			Msg("Warnings during service update")
	}

	err = utils.WaitUntil(ctx, func() (bool, error) {
		log.Debug().
			Str("serviceId", service.ID).
			Msg("Waiting for service update to complete")

		inspectService, _, err := dockerCli.ServiceInspectWithRaw(ctx, service.ID, types.ServiceInspectOptions{})
		if err != nil {
			log.Err(err).
				Str("serviceId", service.ID).
				Msg("Unable to inspect service")
			return false, nil
		}

		if inspectService.UpdateStatus == nil {
			log.Warn().Msg("Service update status is empty")

			return false, nil
		}

		switch inspectService.UpdateStatus.State {
		case swarm.UpdateStateRollbackCompleted:
			if options.ExtendedHealthCheck {
				if err := execRollbackDB(ctx, dockerCli, inspectService, originalImage, 5*time.Minute); err != nil {
					log.Err(err).Msg("Database rollback failed after service rollback")

					return true, errors.New("The update failed, the service was rolled back, but the database rollback failed. Manual rollback might be required")
				}
			}

			return true, errors.New("The update failed and the service was rolled back")
		case swarm.UpdateStateCompleted:
			return true, nil
		default:
			return false, nil
		}
	}, awaitCompletion, 5*time.Second)

	if err != nil {
		log.Err(err).
			Str("serviceId", service.ID).
			Msg("Unable to wait for service update to complete")
		return errUpdateFailure
	}

	log.Info().
		Str("serviceId", service.ID).
		Str("image", imageName).
		Msg("Update process completed")

	return nil
}

func pullImage(ctx context.Context, dockerCli client.APIClient, imageName string) (bool, error) {
	if os.Getenv("SKIP_PULL") != "" {
		return false, nil
	}

	log.Debug().
		Str("image", imageName).
		Msg("Pulling Docker image")

	imagePullOptions, err := dockerstandalone.MakeImagePullOptions()
	if err != nil {
		return false, fmt.Errorf("unable to make image pull options: %w", err)
	}

	reader, err := dockerCli.ImagePull(ctx, imageName, imagePullOptions)
	if err != nil {
		log.Err(err).
			Str("image", imageName).
			Msg("Unable to pull image")

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

func isPortainerHealthCheck(healthConfig *container.HealthConfig) bool {
	if healthConfig == nil {
		return false
	}

	return len(healthConfig.Test) == 3 &&
		healthConfig.Test[0] == "CMD" &&
		healthConfig.Test[1] == "/portainer" &&
		healthConfig.Test[2] == "--health-check"
}

// execRollbackDB executes the database rollback in a separate container.
// Since portainer-updater is running on the same node as the Portainer service, we can
// create a sidecar container and mount the same volumes as the Portainer service.
// This way, we can ensure that the database rollback is executed on the same data as the Portainer service.
func execRollbackDB(ctx context.Context, dockerCli client.APIClient, service swarm.Service, sidecarImage string, timeout time.Duration) error {
	log.Info().Msg("Executing database rollback")

	originalReplicas := uint64(1)
	if service.Spec.Mode.Replicated != nil && service.Spec.Mode.Replicated.Replicas != nil {
		originalReplicas = *service.Spec.Mode.Replicated.Replicas
	}
	// This timeout is not accounted for in the overall update timeout.
	scalingTimeout := 30 * time.Second

	defer func() {
		if scaleErr := Scale(ctx, dockerCli, service.ID, originalReplicas, scalingTimeout); scaleErr != nil {
			log.Err(scaleErr).Msg("unable to reset service to original replica count after failed rollback")
		}
	}()

	if err := Scale(ctx, dockerCli, service.ID, 0, scalingTimeout); err != nil {
		return fmt.Errorf("unable to scale portainer to 0 replicas: %w", err)
	}

	containerConfig := &container.Config{
		Image:      sidecarImage,
		Cmd:        []string{"/portainer", "--force-rollback"},
		Entrypoint: []string{},
	}
	hostConfig := &container.HostConfig{
		Mounts: service.Spec.TaskTemplate.ContainerSpec.Mounts,
	}
	rollbackContainer, err := dockerCli.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, "portainer-db-rollback")
	if err != nil {
		return fmt.Errorf("unable to create rollback container: %w", err)
	}

	if err := dockerstandalone.RollbackDB(ctx, dockerCli, rollbackContainer.ID, timeout); err != nil {
		return fmt.Errorf("unable to rollback database container: %w", err)
	}

	log.Info().Msg("Database rollback completed successfully")

	return nil
}
