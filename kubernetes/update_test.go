package kubernetes

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestUpdateDeployment(t *testing.T) {
	client := fake.NewSimpleClientset()

	deployment := setUpDeployment()

	_, err := client.AppsV1().Deployments("portainer").Create(t.Context(), deployment, metaV1.CreateOptions{})
	require.NoError(t, err)

	watcher := watch.NewFake()
	// Prepend the fake watch reactor
	client.PrependWatchReactor("deployments", func(action k8stesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, watcher, nil
	})
	// Simulate the watch behavior. Succeed the updater after three modifications and event updates.
	go func() {
		calls := 0
		for {
			deployment.Status.ObservedGeneration = deployment.Generation
			if calls < 3 {
				deployment.Status.UpdatedReplicas = 0
				deployment.Status.ReadyReplicas = 0
				deployment.Status.AvailableReplicas = 0
				watcher.Modify(deployment)
				calls++
				time.Sleep(100 * time.Millisecond)
				continue
			}

			deployment.Status.UpdatedReplicas = 1
			deployment.Status.ReadyReplicas = 1
			deployment.Status.AvailableReplicas = 1
			watcher.Modify(deployment)
			watcher.Stop()

			return
		}
	}()

	err = Update(t.Context(), client, "new-image", deployment, "LICENSE123", UpdateOptions{
		ExtendedHealthCheck: false,
	})
	require.NoError(t, err)

	updated, err := client.AppsV1().Deployments("portainer").Get(t.Context(), "portainer", metaV1.GetOptions{})
	require.NoError(t, err)

	assert.Equal(t, "new-image", updated.Spec.Template.Spec.Containers[0].Image)
	assert.Equal(t, "LICENSE123", updated.Spec.Template.Spec.Containers[0].Env[0].Value)
	assert.Equal(t, int32(1), *updated.Spec.Replicas)
	assert.Equal(t, int64(1), updated.Status.ObservedGeneration)
	assert.Equal(t, int32(1), updated.Status.UpdatedReplicas)
	assert.Equal(t, int32(1), updated.Status.ReadyReplicas)
	assert.Equal(t, int32(1), updated.Status.AvailableReplicas)
}

func TestUpdateWithExtendedHealthCheckTimeout(t *testing.T) {
	// This test asserts that the Update function returns errUpdateFailure
	// if the deployment does not become ready within the extended timeout
	// period when Portainer Auto Update is enabled.
	//
	// We simulate this by not updating the deployment status to ready
	// in the fake watch reactor and subsequently closing the watcher. Although not perfect, as the timeout
	// is set in the Update function, this ensures we can assert timeout behaviour.

	client := fake.NewSimpleClientset()
	deployment := setUpDeployment()
	_, err := client.AppsV1().Deployments("portainer").Create(t.Context(), deployment, metaV1.CreateOptions{})
	require.NoError(t, err)
	zerolog.SetGlobalLevel(zerolog.Disabled)
	t.Cleanup(func() {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	})

	synctest.Test(t, func(t *testing.T) {
		watcher := watch.NewFake()
		client.PrependWatchReactor("deployments", func(action k8stesting.Action) (handled bool, ret watch.Interface, err error) {
			return true, watcher, nil
		})
		go func() {
			deployment.Status.ObservedGeneration = deployment.Generation
			deployment.Status.UpdatedReplicas = 1
			deployment.Status.ReadyReplicas = 0
			deployment.Status.AvailableReplicas = 0
			watcher.Modify(deployment)

			time.Sleep(100 * time.Millisecond)
			watcher.Stop()
		}()

		err = Update(t.Context(), client, "new-image", deployment, "LICENSE123", UpdateOptions{
			ExtendedHealthCheck: true,
		})
		require.ErrorIs(t, errUpdateFailure, err)
	})
}

func TestCreateEnvVarPatch(t *testing.T) {
	t.Run("No existing env vars", func(t *testing.T) {
		patch := createEnvVarPatch("LICENSE123", nil)

		assert.Equal(t, "add", patch.Op)

		vals, ok := patch.Value.([]coreV1.EnvVar)
		require.True(t, ok)

		require.Len(t, vals, 1)
		assert.Equal(t, "PORTAINER_LICENSE_KEY", vals[0].Name)
		assert.Equal(t, "LICENSE123", vals[0].Value)
	})

	t.Run("Existing env vars", func(t *testing.T) {
		envVars := []coreV1.EnvVar{
			{Name: "EXISTING", Value: "foo"},
		}

		patch := createEnvVarPatch("LICENSE123", envVars)

		assert.Equal(t, "add", patch.Op)

		val, ok := patch.Value.(coreV1.EnvVar)
		require.True(t, ok)

		assert.Equal(t, "PORTAINER_LICENSE_KEY", val.Name)
		assert.Equal(t, "LICENSE123", val.Value)
	})
}

func setUpDeployment() *appV1.Deployment {
	replicas := int32(1)
	return &appV1.Deployment{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      "portainer",
			Namespace: "portainer",
			UID:       types.UID("12345"),
		},
		Spec: appV1.DeploymentSpec{
			Replicas: &replicas,
			Template: coreV1.PodTemplateSpec{
				Spec: coreV1.PodSpec{
					Containers: []coreV1.Container{
						{Name: "portainer", Image: "old-image"},
					},
				},
			},
		},
		Status: appV1.DeploymentStatus{
			ObservedGeneration: 1,
			UpdatedReplicas:    1,
			ReadyReplicas:      1,
			AvailableReplicas:  1,
		},
	}
}
