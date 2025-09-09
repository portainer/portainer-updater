package kubernetes

import (
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func GetClient() (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		if os.Getenv("PORTAINER_UPDATER_DEBUG") != "1" {
			return nil, errors.WithMessage(err, "failed to get kubernetes config")
		}
		// Fallback to local kubeconfig
		kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, errors.WithMessage(err, "failed to get kubernetes config")
		}
	}

	return kubernetes.NewForConfig(config)
}
