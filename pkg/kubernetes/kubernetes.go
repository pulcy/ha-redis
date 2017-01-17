package kubernetes

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	k8s "github.com/YakLabs/k8s-client"
	"github.com/YakLabs/k8s-client/http"
	"github.com/juju/errgo"
)

const (
	namespacePath       = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	createdByAnnotation = "kubernetes.io/created-by"
)

var (
	maskAny = errgo.MaskFunc(errgo.Any)
)

// SerializedReference is a reference to serialized object.
type SerializedReference struct {
	k8s.TypeMeta `json:",inline"`
	// The reference to an object in the system.
	// +optional
	Reference k8s.ObjectReference `json:"reference,omitempty"`
}

// IsRunningInKubernetes returns true if a Kubernetes environment is detected.
func IsRunningInKubernetes() bool {
	if os.Getenv("KUBERNETES_SERVICE_HOST") == "" {
		return false
	}
	if os.Getenv("KUBERNETES_SERVICE_PORT") == "" {
		return false
	}
	if GetNamespace() == "" {
		return false
	}
	return true
}

// GetNamespace loads the namespace of the pod running this process.
// If the namespace is not found, (e.g. because the process is not running in kubernetes)
// an empty string is returned.
func GetNamespace() string {
	raw, err := ioutil.ReadFile(namespacePath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// GetCreatingReplicaSetName returns the name of the replicaSet that created the pod with given name.
func GetCreatingReplicaSetName(namespace, podName string) (string, error) {
	c, err := http.NewInCluster()
	if err != nil {
		return "", maskAny(err)
	}
	pod, err := c.GetPod(namespace, podName)
	if err != nil {
		return "", maskAny(err)
	}
	ann, ok := pod.Annotations[createdByAnnotation]
	if !ok {
		return "", maskAny(fmt.Errorf("Annotation '%s' not found", createdByAnnotation))
	}
	var sref SerializedReference
	if err := json.Unmarshal([]byte(ann), &sref); err != nil {
		return "", maskAny(err)
	}
	if sref.Reference.Kind != "ReplicaSet" {
		return "", maskAny(fmt.Errorf("Not created by a ReplicaSet (but '%s')", sref.Reference.Kind))
	}
	return sref.Reference.Name, nil
}

// GetPodIP returns the IP address of the the pod with given name.
func GetPodIP(namespace, podName string) (string, error) {
	c, err := http.NewInCluster()
	if err != nil {
		return "", maskAny(err)
	}
	pod, err := c.GetPod(namespace, podName)
	if err != nil {
		return "", maskAny(err)
	}
	return pod.Status.PodIP, nil
}
