// Package kube initializes the Kubernetes client used by minato-server.
package kube

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// NewClient returns a clientset using the in-cluster config when running
// inside a pod, falling back to the local kubeconfig for development.
func NewClient() (*kubernetes.Clientset, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			home, herr := os.UserHomeDir()
			if herr != nil {
				return nil, fmt.Errorf("resolve kubeconfig: %w", herr)
			}
			kubeconfig = filepath.Join(home, ".kube", "config")
		}
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("load kubeconfig: %w", err)
		}
	}
	return kubernetes.NewForConfig(cfg)
}
