package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/require"
	appV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFindPortainerDeployment(t *testing.T) {
	t.Parallel()
	cli := fake.NewSimpleClientset(
		setUpPortainerDeployment(),
	)

	portainerDeployment, err := FindPortainerDeployment(t.Context(), cli)

	require.NoError(t, err)
	require.Equal(t, "portainer", portainerDeployment.Name)
	require.Equal(t, "portainer", portainerDeployment.Namespace)
}

func TestFindPortainerDeploymentNotFound(t *testing.T) {
	t.Parallel()
	cli := fake.NewSimpleClientset()
	_, err := FindPortainerDeployment(t.Context(), cli)

	require.Error(t, err)
	require.Equal(t, "no deployments found", err.Error())
}

func TestFindPortainerDeploymentMultipleDeployments(t *testing.T) {
	t.Parallel()
	old := setUpPortainerDeployment()
	old.Name = "portainer-old"

	cli := fake.NewSimpleClientset(
		setUpPortainerDeployment(),
		old,
	)

	portainerDeployment, err := FindPortainerDeployment(t.Context(), cli)

	require.Error(t, err)
	require.Nil(t, portainerDeployment)
	require.Equal(t, "multiple deployments found", err.Error())
}

func setUpPortainerDeployment() *appV1.Deployment {
	replicas := int32(1)
	return &appV1.Deployment{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      "portainer",
			Namespace: "portainer",
			Labels: map[string]string{
				"app.kubernetes.io/name": "portainer",
			},
		},
		Spec: appV1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metaV1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name": "portainer",
				},
			},
			Template: coreV1.PodTemplateSpec{
				ObjectMeta: metaV1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name": "portainer",
					},
				},
			},
		},
	}
}
