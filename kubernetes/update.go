package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/pkg/errors"
	"github.com/portainer/portainer-updater/utils"
	"github.com/rs/zerolog/log"
	appV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

type (
	templateSpec struct {
		Containers []coreV1.Container `json:"containers"`
	}
	patchBodySpecTemplate struct {
		Spec templateSpec `json:"spec"`
	}
	patchBodySpec struct {
		Template patchBodySpecTemplate `json:"template"`
	}
	patchBody struct {
		Spec patchBodySpec `json:"spec"`
	}
)

var errUpdateFailure = errors.New("update failure")

func Update(ctx context.Context, cli *kubernetes.Clientset, imageName string, deployment *appV1.Deployment, updateConfig func(containerSpc coreV1.Container)) error {
	log.Info().
		Str("deploymentName", deployment.Name).
		Str("image", imageName).
		Msg("Starting update process")

	podsQuery, err := cli.CoreV1().Pods(deployment.Namespace).List(ctx, metaV1.ListOptions{LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", deployment.Name)})
	if err != nil {
		return errors.WithMessage(err, "unable to list pods")
	}

	if len(podsQuery.Items) == 0 || len(podsQuery.Items[0].Spec.Containers) == 0 {
		return errors.New("no pods found")
	}

	podContainer := podsQuery.Items[0].Spec.Containers[0]

	// log.Debug().
	// 	Str("image", imageName).
	// 	Str("containerImage", podsQuery.Items[0].Spec.Containers[0].Image).
	// 	Msg("Checking whether the latest image is available")

	// imageUpToDate, err := pullImage(ctx, cli, imageName)
	// if err != nil {
	// 	log.Err(err).
	// 		Msg("Unable to pull image")

	// 	return errUpdateFailure
	// }

	// if deployment.Spec.TaskTemplate.ContainerSpec.Image == imageName && imageUpToDate {
	// 	log.Info().
	// 		Str("image", imageName).
	// 		Str("deploymentName", deployment.ID).
	// 		Msg("Image is already up to date, shutting down")

	// 	return nil
	// }

	containerConfig := coreV1.Container{
		Name:  podContainer.Name,
		Image: imageName,
		Env:   podContainer.Env,
	}

	if os.Getenv("SKIP_PULL") != "" {
		containerConfig.ImagePullPolicy = coreV1.PullNever
	}

	updateConfig(containerConfig)

	patchBody := patchBody{
		Spec: patchBodySpec{
			Template: patchBodySpecTemplate{
				Spec: templateSpec{
					Containers: []coreV1.Container{
						containerConfig,
					},
				},
			},
		},
	}

	json, err := json.Marshal(patchBody)
	if err != nil {
		return errors.WithMessage(err, "unable to marshal patch body")
	}
	newDeployment, err := cli.AppsV1().
		Deployments(deployment.Namespace).
		Patch(ctx, deployment.Name, types.MergePatchType, json, metaV1.PatchOptions{})
	if err != nil {
		return errors.WithMessage(err, "unable to patch deployment")
	}

	// 	if len(updateResponse.Warnings) > 0 {
	// 		log.Warn().
	// 			Str("deploymentName", deployment.ID).
	// 			Interface("warnings", updateResponse.Warnings).
	// 			Msg("Warnings during deployment update")
	// 	}

	err = utils.WaitUntil(ctx, func() bool {
		log.Debug().
			Str("deploymentName", newDeployment.Name).
			Msg("Waiting for deployment update to complete")

		return newDeployment.Status.AvailableReplicas >= 1
	}, 1*time.Minute, 5*time.Second)

	if err != nil {
		log.Err(err).
			Str("deploymentName", deployment.Name).
			Msg("Unable to wait for deployment update to complete")
		return errUpdateFailure
	}

	log.Info().
		Str("deploymentName", deployment.Name).
		Str("image", imageName).
		Msg("Update process completed")

	return nil
}
