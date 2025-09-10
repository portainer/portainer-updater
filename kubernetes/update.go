package kubernetes

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/segmentio/encoding/json"
	appV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

var errUpdateFailure = errors.New("update failure")

type UpdateOptions struct {
	ExtendedHealthCheck bool
}

func Update(ctx context.Context, cli kubernetes.Interface, imageName string, deployment *appV1.Deployment, licenseKey string, options UpdateOptions) error {
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

	defaultTimeout := int64((5 * time.Minute).Seconds()) // 5 minutes by default
	timeout := defaultTimeout

	if options.ExtendedHealthCheck {
		patch = append(patch, createHealthCheckPatch())
		timeout = int64((3 * time.Hour).Seconds()) // 3 hours if Portainer Auto Update is enabled, to allow for long migrations
	}

	err := updateDeployment(ctx, deployCli, deployment.Name, imageName, patch, &timeout)
	if err != nil {
		// Reset patch so that new patches can be applied cleanly
		patch = []jsonPatch{}
		log.Err(err).
			Str("deploymentName", deployment.Name).
			Msg("Unable to update deployment")

		log.Info().
			Str("deploymentName", deployment.Name).
			Msg("Rolling back deployment")

		if options.ExtendedHealthCheck {
			patch = append(patch, removeHealthCheckPatch())
		}

		err := updateDeployment(ctx, deployCli, deployment.Name, originalImage, patch, &defaultTimeout)
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

func createHealthCheckPatch() jsonPatch {
	return jsonPatch{
		Op:   "add",
		Path: "/spec/template/spec/containers/0/readinessProbe",
		Value: map[string]interface{}{
			"exec": map[string]interface{}{
				"command": []string{"/portainer", "--health-check"},
			},
			"initialDelaySeconds": 5,
			"periodSeconds":       5,
			"timeoutSeconds":      5,
			// A finite number of retries is supported.
			// Each retry takes in the best case a few milliseconds and worst case 5 seconds, and is run every 5 seconds.
			// Because migrations can take a long time, we want to allow it to run for 2 hours.
			// So 2 hours / 5 seconds = 1440 retries, for the quickest possible retry.
			// And 2 hours / 10 seconds = 720 retries, for the slowest possible retry.
			// It's unlikely that the healthcheck will take the full 5 seconds every time, so 1000 retries should be sufficient.
			// Thus, the maximum time the health check can take is 1000 * 10 seconds = 10000 seconds = ~2.78 hours.
			"failureThreshold": 1000,
		},
	}
}

func removeHealthCheckPatch() jsonPatch {
	return jsonPatch{
		Op:   "remove",
		Path: "/spec/template/spec/containers/0/readinessProbe",
	}
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

func updateDeployment(ctx context.Context, deployCli v1.DeploymentInterface, deploymentName, imageName string, morePatch []jsonPatch, timeoutSeconds *int64) error {
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

	return waitForDeployment(ctx, deployCli, newDeployment.Name, newDeployment.UID, timeoutSeconds)
}

func waitForDeployment(ctx context.Context, deployCli v1.DeploymentInterface, deploymentName string, uid types.UID, timeoutSeconds *int64) error {
	watcher, err := deployCli.Watch(ctx, metaV1.ListOptions{
		FieldSelector:  fmt.Sprintf("metadata.name=%s", deploymentName),
		TimeoutSeconds: timeoutSeconds,
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

		specReplicas := int32(1)
		if deployment.Spec.Replicas != nil {
			specReplicas = *deployment.Spec.Replicas
		}

		deploymentOk := deployment.Status.ObservedGeneration >= deployment.Generation && // Ensure the deployment controller has processed the latest generation. This avoids a race condition caused by the deployment controller not having processed the latest generation yet.
			deployment.Status.UpdatedReplicas == specReplicas &&
			deployment.Status.ReadyReplicas == specReplicas &&
			deployment.Status.AvailableReplicas == specReplicas

		if deploymentOk {
			return nil
		}
	}

	return errors.New("timeout")
}
