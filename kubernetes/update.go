package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
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
	jsonPatch struct {
		Op    string      `json:"op"`
		Path  string      `json:"path"`
		Value interface{} `json:"value"`
	}
)

var errUpdateFailure = errors.New("update failure")

func Update(ctx context.Context, cli *kubernetes.Clientset, imageName string, deployment *appV1.Deployment, licenseKey string) error {
	log.Info().
		Str("deploymentName", deployment.Name).
		Str("image", imageName).
		Str("license", licenseKey).
		Msg("Starting update process")

	patch := []jsonPatch{
		{
			Op:    "replace",
			Path:  "/spec/template/spec/containers/0/image",
			Value: imageName,
		},
	}

	if licenseKey != "" {
		patch = append(patch, jsonPatch{
			Op:   "add",
			Path: "/spec/template/spec/containers/0/env",
			Value: []coreV1.EnvVar{
				{
					Name:  "PORTAINER_LICENSE_KEY",
					Value: licenseKey,
				},
			},
		})
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return errors.WithMessage(err, "unable to marshal patch")
	}

	deployCli := cli.AppsV1().
		Deployments(deployment.Namespace)

	newDeployment, err := deployCli.
		Patch(ctx, deployment.Name, types.JSONPatchType, patchBytes, metaV1.PatchOptions{})
	if err != nil {
		return errors.WithMessage(err, "unable to patch deployment")
	}
	err = utils.WaitUntil(ctx, func() bool {
		log.Debug().
			Str("deploymentName", newDeployment.Name).
			Msg("Waiting for deployment update to complete")

		i, err := deployCli.Watch(ctx, metaV1.ListOptions{
			FieldSelector: fmt.Sprintf("metadata.id=%s", newDeployment.UID),
		})
		if err != nil {
			log.Err(err).
				Str("deploymentName", newDeployment.Name).
				Msg("Unable to watch deployments")

			return false
		}

		for event := range i.ResultChan() {
			deployment, ok := event.Object.(*appV1.Deployment)
			if !ok {
				continue
			}

			for _, condition := range deployment.Status.Conditions {
				if condition.Type == appV1.DeploymentReplicaFailure && condition.Status == coreV1.ConditionTrue {
					log.Error().
						Str("deploymentName", newDeployment.Name).
						Str("reason", condition.Message).
						Msg("Unable to wait for deployment update to complete")
					return false
				}

				if condition.Type == appV1.DeploymentAvailable && condition.Status == coreV1.ConditionTrue {
					return true
				}
			}
		}

		return false
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
