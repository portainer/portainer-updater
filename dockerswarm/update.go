package dockerswarm

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/portainer/portainer-updater/utils"
	"github.com/rs/zerolog/log"
)

var errUpdateFailure = errors.New("update failure")
var errPullImageFailure = errors.New("pull image failure")

func Update(ctx context.Context, dockerCli *client.Client, imageName string, service *swarm.Service, updateConfig func(*swarm.ContainerSpec)) error {
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

		return err
	}

	if service.Spec.TaskTemplate.ContainerSpec.Image == imageName && imageUpToDate {
		log.Info().
			Str("image", imageName).
			Str("serviceId", service.ID).
			Msg("Image is already up to date, shutting down")

		return nil
	}

	service.Spec.TaskTemplate.ContainerSpec.Image = imageName

	updateConfig(service.Spec.TaskTemplate.ContainerSpec)
	prevVersion := service.Meta.Version
	service.Meta.Version = swarm.Version{Index: service.Meta.Version.Index + 1}

	service.Spec.UpdateConfig = &swarm.UpdateConfig{
		FailureAction: swarm.UpdateFailureActionRollback,
		Order:         swarm.UpdateOrderStopFirst,
	}

	updateResponse, err := dockerCli.ServiceUpdate(ctx, service.ID, prevVersion, service.Spec, types.ServiceUpdateOptions{})
	if err != nil {
		return errors.WithMessage(err, errUpdateFailure.Error())
	}

	if len(updateResponse.Warnings) > 0 {
		log.Warn().
			Str("serviceId", service.ID).
			Interface("warnings", updateResponse.Warnings).
			Msg("Warnings during service update")
	}

	err = utils.WaitUntil(ctx, func() bool {
		log.Debug().
			Str("serviceId", service.ID).
			Msg("Waiting for service update to complete")

		service, _, err := dockerCli.ServiceInspectWithRaw(ctx, service.ID, types.ServiceInspectOptions{})
		if err != nil {
			log.Err(err).
				Str("serviceId", service.ID).
				Msg("Unable to inspect service")
			return false
		}

		return service.UpdateStatus != nil && service.UpdateStatus.State == swarm.UpdateStateCompleted
	}, 1*time.Minute, 5*time.Second)

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

func pullImage(ctx context.Context, dockerCli *client.Client, imageName string) (bool, error) {
	if os.Getenv("SKIP_PULL") == "1" {
		// When the target image is not existed on both remote and local, the
		// existing portainer service can be mistakenly shutdown by docker
		// swarm due to this fast failure, so it is better to check if the
		// env "SKIP_PULL" is explicitly set to 1
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

		return false, errPullImageFailure
	}
	defer reader.Close()

	// We have to read the output of the ImagePull command - otherwise it will be done asynchronously
	// This is not really well documented on the Docker SDK
	var imagePullOutputBuf bytes.Buffer
	tee := io.TeeReader(reader, &imagePullOutputBuf)

	io.Copy(os.Stdout, tee)
	io.Copy(&imagePullOutputBuf, reader)

	// TODO: REVIEW
	// There might be a cleaner way to check whether the container is using the same image as the one available locally
	// Maybe through image digest validation instead of checking the output of the docker pull command
	return strings.Contains(imagePullOutputBuf.String(), "Image is up to date"), nil
}
