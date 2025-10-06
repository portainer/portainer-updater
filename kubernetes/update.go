package kubernetes

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/segmentio/encoding/json"
	appV1 "k8s.io/api/apps/v1"
	batchV1 "k8s.io/api/batch/v1"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	appsV1Client "k8s.io/client-go/kubernetes/typed/apps/v1"
	batchV1Client "k8s.io/client-go/kubernetes/typed/batch/v1"
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

	deployCli := cli.AppsV1().Deployments(deployment.Namespace)
	jobCli := cli.BatchV1().Jobs(deployment.Namespace)

	var patch []jsonPatch
	if licenseKey != "" {
		patch = append(patch, createEnvVarPatch(licenseKey, deployment.Spec.Template.Spec.Containers[0].Env))
	}

	if os.Getenv("REGISTRY_USED") != "" {
		imagePullSecretPatch := createImagePullSecretPatch(os.Getenv("REGISTRY_PULL_SECRET_NAME"), deployment.Spec.Template.Spec.ImagePullSecrets)
		patch = append(patch, imagePullSecretPatch)
	}

	defaultTimeout := int64((5 * time.Minute).Seconds()) // 5 minutes by default
	timeout := defaultTimeout

	if options.ExtendedHealthCheck {
		patch = append(patch, createHealthCheckPatch())
		timeout = int64((3 * time.Hour).Seconds()) // 3 hours if Portainer Auto Update is enabled, to allow for long migrations
	}

	err := updateDeployment(ctx, deployCli, deployment.Name, imageName, patch, &timeout)
	if err == nil {
		log.Info().
			Str("deploymentName", deployment.Name).
			Str("image", imageName).
			Msg("Update process completed")

		return nil
	}

	log.Err(err).
		Str("deploymentName", deployment.Name).
		Msg("Unable to update deployment")

	log.Info().
		Str("deploymentName", deployment.Name).
		Msg("Rolling back deployment")

	// Reset patch so that new patches can be applied cleanly
	patch = []jsonPatch{}

	if options.ExtendedHealthCheck {
		patch = append(patch, removeHealthCheckPatch())
		if err := rollbackDB(ctx, deployCli, jobCli, deployment, 5*time.Minute); err != nil {
			log.Err(err).
				Str("deploymentName", deployment.Name).
				Msg("Database rollback failed. Manual rollback might be required")
		}
	}

	if err := updateDeployment(ctx, deployCli, deployment.Name, originalImage, patch, &defaultTimeout); err != nil {
		log.Err(err).
			Str("deploymentName", deployment.Name).
			Msg("Unable to rollback deployment")
	}

	return errUpdateFailure
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

func createImagePullSecretPatch(secretName string, existing []coreV1.LocalObjectReference) jsonPatch {
	if secretName == "" {
		return jsonPatch{}
	}

	for _, ref := range existing {
		if ref.Name == secretName {
			return jsonPatch{}
		}
	}

	if existing == nil {
		return jsonPatch{
			Op:   "add",
			Path: "/spec/template/spec/imagePullSecrets",
			Value: []coreV1.LocalObjectReference{
				{Name: secretName},
			},
		}
	}

	return jsonPatch{
		Op:    "add",
		Path:  "/spec/template/spec/imagePullSecrets/-",
		Value: coreV1.LocalObjectReference{Name: secretName},
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

func updateDeployment(ctx context.Context, deployCli appsV1Client.DeploymentInterface, deploymentName, imageName string, morePatch []jsonPatch, timeoutSeconds *int64) error {
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

func waitForDeployment(ctx context.Context, deployCli appsV1Client.DeploymentInterface, deploymentName string, uid types.UID, timeoutSeconds *int64) error {
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

func rollbackDB(ctx context.Context, deployCli appsV1Client.DeploymentInterface, jobCli batchV1Client.JobInterface, deployment *appV1.Deployment, timeout time.Duration) error {
	log.Info().Msg("Rolling back DB")
	var originalReplicas int32 = 1
	if deployment.Spec.Replicas != nil {
		originalReplicas = *deployment.Spec.Replicas
	}

	scalingTimeout := 30 * time.Second

	// Defer a reset of the deployment replicas to original value
	defer func() {
		if scaleErr := scaleDeployment(ctx, deployCli, deployment.Name, originalReplicas, scalingTimeout); scaleErr != nil {
			log.Err(scaleErr).
				Str("deploymentName", deployment.Name).
				Msg("failed to scale deployment back to original replicas after rollback job")
		}
	}()

	// Scale deployment to 0
	zero := int32(0)
	if err := scaleDeployment(ctx, deployCli, deployment.Name, zero, scalingTimeout); err != nil {
		return fmt.Errorf("failed to scale deployment back to zero for the rollback job: %w", err)
	}

	// Create and run the rollback job
	rollbackJobSpec := createRollbackJobSpec(fmt.Sprintf("%s-db-rollback-job-%d", deployment.Name, time.Now().Unix()), deployment, timeout)
	rollbackJob, err := jobCli.Create(ctx, rollbackJobSpec, metaV1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("creating rollback job: %w", err)
	}

	defer func() {
		// Delete the job to free resources, but don't block on it.
		// If it fails, it will be cleaned up automatically after TTLSecondsAfterFinished.
		if deleteJobErr := deleteJob(ctx, jobCli, rollbackJob.Name); deleteJobErr != nil {
			log.Err(deleteJobErr).Msg("failed to delete rollback job. It will be cleaned up automatically after TTLSecondsAfterFinished")
		}
	}()

	if err := awaitRollbackJob(ctx, jobCli, rollbackJob, timeout); err != nil {
		return fmt.Errorf("failed to run rollback job: %w", err)
	}

	return nil
}

func scaleDeployment(ctx context.Context, deployCli appsV1Client.DeploymentInterface, deploymentName string, replicas int32, timeout time.Duration) error {
	deployment, err := deployCli.Get(ctx, deploymentName, metaV1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment: %w", err)
	}

	if deployment.Spec.Replicas != nil && *deployment.Spec.Replicas == replicas {
		return nil
	}

	patch := []jsonPatch{
		{
			Op:    "replace",
			Path:  "/spec/replicas",
			Value: replicas,
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("failed to marshal scale-to-zero patch: %w", err)
	}

	patchedDeployment, err := deployCli.Patch(
		ctx,
		deploymentName,
		types.JSONPatchType,
		patchBytes,
		metaV1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to patch deployment: %w", err)
	}

	timeoutSeconds := int64(timeout.Seconds())

	if err := waitForDeployment(ctx, deployCli, deploymentName, patchedDeployment.UID, &timeoutSeconds); err != nil {
		return fmt.Errorf("failed to wait for deployment to scale: %w", err)
	}

	return nil
}

func deleteJob(ctx context.Context, jobCli batchV1Client.JobInterface, jobName string) error {
	// PropagationPolicy to background to not block on deleting pods; best-effort
	deletePolicy := metaV1.DeletePropagationBackground

	err := jobCli.Delete(ctx, jobName, metaV1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	})
	if err != nil {
		return fmt.Errorf("failed to delete job: %w", err)
	}

	return nil
}

func createRollbackJobSpec(name string, deployment *appV1.Deployment, timeout time.Duration) *batchV1.Job {
	portainerContainer := deployment.Spec.Template.Spec.Containers[0]
	// It's unlikely that a rollback will fail, but to account for flakes we allow 3 retries.
	// Furthermore, force-rollback will replace the existing DB with the last backup, and since the backup
	// is not changed between retries, retrying is safe.
	backoffLimit := int32(3)

	// In the unlikely event that the clean-up fails, we don't want jobs to pile up.
	// The job should be short-lived anyway.
	// We set it to 5 minutes, which should be more than enough time for the job to complete and be inspected if needed.
	ttlSecondsAfterFinished := int32(300)

	jobTimeout := int64(timeout.Seconds())

	return &batchV1.Job{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: deployment.Namespace,
		},
		Spec: batchV1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttlSecondsAfterFinished,
			Template: coreV1.PodTemplateSpec{
				Spec: coreV1.PodSpec{
					RestartPolicy:         coreV1.RestartPolicyOnFailure,
					Volumes:               deployment.Spec.Template.Spec.Volumes,
					ActiveDeadlineSeconds: &jobTimeout,
					ServiceAccountName:    "portainer-sa-clusteradmin",
					Containers: []coreV1.Container{
						{
							Name:         "portainer-db-rollback",
							Image:        portainerContainer.Image,
							Command:      []string{"/portainer", "--force-rollback"}, // Takes precedence over ENTRYPOINT
							VolumeMounts: portainerContainer.VolumeMounts,
						},
					},
				},
			},
		},
	}
}

func awaitRollbackJob(ctx context.Context, jobCli batchV1Client.JobInterface, job *batchV1.Job, timeout time.Duration) error {
	timeoutSeconds := int64(timeout.Seconds())

	watcher, err := jobCli.Watch(ctx, metaV1.ListOptions{
		FieldSelector:  fmt.Sprintf("metadata.name=%s", job.Name),
		TimeoutSeconds: &timeoutSeconds,
	})
	if err != nil {
		return fmt.Errorf("failed to create job watcher: %w", err)
	}
	defer watcher.Stop()

	for event := range watcher.ResultChan() {
		eventJob, ok := event.Object.(*batchV1.Job)
		if !ok {
			continue // ignore other objects
		}

		for _, condition := range eventJob.Status.Conditions {
			if condition.Type == batchV1.JobComplete && condition.Status == coreV1.ConditionTrue {
				return nil
			}

			if condition.Type == batchV1.JobFailed && condition.Status == coreV1.ConditionTrue {
				return fmt.Errorf("job %s failed: %s", job.Name, condition.Message)
			}
		}
	}

	return errors.New("job timed out")
}
