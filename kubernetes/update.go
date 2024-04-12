package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	appV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/kubernetes/typed/apps/v1"
)

type (
	jsonPatch struct {
		Op    string      `json:"op"`
		Path  string      `json:"path"`
		Value interface{} `json:"value"`
	}
)

const fiveMinutes = int64(300)

var errUpdateFailure = errors.New("update failure")

func Update(ctx context.Context, cli *kubernetes.Clientset, imageName string, deployment *appV1.Deployment, licenseKey string) error {
	log.Info().
		Str("deploymentName", deployment.Name).
		Str("image", imageName).
		Msg("Starting update process")

	originalImage := deployment.Spec.Template.Spec.Containers[0].Image

	deployCli := cli.AppsV1().
		Deployments(deployment.Namespace)

	var patch []jsonPatch
	if licenseKey != "" {
		patch = append(patch, createEnvVarPatch(licenseKey, deployment.Spec.Template.Spec.Containers[0].Env))
	}

	err := updateDeployment(ctx, deployCli, deployment.Name, imageName, patch)
	if err != nil {
		log.Err(err).
			Str("deploymentName", deployment.Name).
			Msg("Unable to update deployment")

		log.Info().
			Str("deploymentName", deployment.Name).
			Msg("Rolling back deployment")

		err := updateDeployment(ctx, deployCli, deployment.Name, originalImage, nil)
		if err != nil {
			log.Err(err).
				Str("deploymentName", deployment.Name).
				Msg("Unable to rollback deployment")
		}

		return errUpdateFailure
	}

	log.Info().
		Str("deploymentName", deployment.Name).
		Str("image", imageName).
		Msg("Update process completed")

	return nil
}

func createEnvVarPatch(licenseKey string, envVars []coreV1.EnvVar) jsonPatch {
	licenseKeyEnvVar := coreV1.EnvVar{
		Name:  "PORTAINER_LICENSE_KEY",
		Value: licenseKey,
	}

	if envVars == nil {
		return jsonPatch{
			Op:   "add",
			Path: "/spec/template/spec/containers/0/env",
			Value: []coreV1.EnvVar{
				licenseKeyEnvVar,
			},
		}
	}

	index, found := Index(envVars, func(e coreV1.EnvVar) bool {
		return e.Name == licenseKeyEnvVar.Name
	})

	if found {
		return jsonPatch{
			Op:    "replace",
			Path:  fmt.Sprintf("/spec/template/spec/containers/0/env/%d", index),
			Value: licenseKeyEnvVar,
		}
	}

	return jsonPatch{
		Op:    "add",
		Path:  "/spec/template/spec/containers/0/env/-",
		Value: licenseKeyEnvVar,
	}

}

func Index[E any](slice []E, predicate func(E) bool) (int, bool) {
	for i, v := range slice {
		if predicate(v) {
			return i, true
		}
	}

	return -1, false
}

func updateDeployment(ctx context.Context, deployCli v1.DeploymentInterface, deploymentName, imageName string, morePatch []jsonPatch) error {
	patch := append([]jsonPatch{
		{
			Op:    "replace",
			Path:  "/spec/template/spec/containers/0/image",
			Value: imageName,
		},
	}, morePatch...)

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return errors.WithMessage(err, "unable to marshal patch")
	}

	newDeployment, err := deployCli.
		Patch(ctx, deploymentName, types.JSONPatchType, patchBytes, metaV1.PatchOptions{})
	if err != nil {
		return errors.WithMessage(err, "unable to patch deployment")
	}

	log.Debug().
		Str("deploymentName", deploymentName).
		Msg("Waiting for deployment to complete")

	return waitForDeployment(ctx, deployCli, newDeployment.Name, newDeployment.UID)
}

func waitForDeployment(ctx context.Context, deployCli v1.DeploymentInterface, deploymentName string, uid types.UID) error {
	// for some reason when we start, we have both updatedReplicas and readyReplicas set to 1
	// we will wait 5 seconds before starting to watch
	time.Sleep(5 * time.Second)

	timeoutSeconds := fiveMinutes
	watcher, err := deployCli.Watch(ctx, metaV1.ListOptions{
		FieldSelector:  fmt.Sprintf("metadata.name=%s", deploymentName),
		TimeoutSeconds: &timeoutSeconds,
	})
	if err != nil {
		log.Err(err).
			Str("deploymentName", deploymentName).
			Str("deploymentUID", string(uid)).
			Msg("Unable to watch deployments")

		return errors.WithMessage(err, "unable to watch deployments")
	}

	for event := range watcher.ResultChan() {
		deployment, ok := event.Object.(*appV1.Deployment)
		if !ok || deployment.UID != uid {
			continue
		}

		for _, condition := range deployment.Status.Conditions {
			if condition.Type == appV1.DeploymentReplicaFailure && condition.Status == coreV1.ConditionTrue {
				log.Error().
					Str("deploymentName", deploymentName).
					Str("reason", condition.Message).
					Msg("Deployment replica failure")
				return errors.New("deployment replica failure")
			}
		}

		log.Debug().
			Int32("ReadyReplicas", deployment.Status.ReadyReplicas).
			Int32("AvailableReplicas", deployment.Status.AvailableReplicas).
			Int32("Replicas", deployment.Status.Replicas).
			Int32("UnavailableReplicas", deployment.Status.UnavailableReplicas).
			Int32("UpdatedReplicas", deployment.Status.UpdatedReplicas).
			Msg("checking replicas condition")

		if deployment.Status.UpdatedReplicas > 0 && deployment.Status.ReadyReplicas > 0 {
			return nil
		}
	}

	return errors.New("timeout")
}
