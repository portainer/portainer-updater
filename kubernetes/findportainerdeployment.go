package kubernetes

import (
	"context"

	"github.com/pkg/errors"
	appV1 "k8s.io/api/apps/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func FindPortainerDeployment(ctx context.Context, cli *kubernetes.Clientset) (*appV1.Deployment, error) {
	list, err := cli.AppsV1().Deployments("portainer").List(ctx, metaV1.ListOptions{LabelSelector: "app.kubernetes.io/name=portainer"})
	if err != nil {
		return nil, errors.WithMessage(err, "failed to list deployments")
	}

	if len(list.Items) == 0 {
		return nil, errors.New("no deployments found")
	}

	if len(list.Items) > 1 {
		return nil, errors.New("multiple deployments found")
	}

	return &list.Items[0], nil

}
