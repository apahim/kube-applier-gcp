package kubeapplier

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// ApplyDesire holds a single Kubernetes object to be server-side-applied to
// the management cluster's apiserver.
type ApplyDesire struct {
	FirestoreMetadata `json:"firestoreMetadata"`
	Spec              ApplyDesireSpec   `json:"spec" firestore:"spec"`
	Status            ApplyDesireStatus `json:"status" firestore:"status"`
}

type ApplyDesireSpec struct {
	ManagementCluster string                `json:"managementCluster" firestore:"managementCluster"`
	ClusterName       string                `json:"clusterName" firestore:"clusterName"`
	NodePoolName      string                `json:"nodePoolName,omitempty" firestore:"nodePoolName,omitempty"`
	TargetItem        ResourceReference     `json:"targetItem" firestore:"targetItem"`
	KubeContent       *runtime.RawExtension `json:"kubeContent,omitempty" firestore:"kubeContent,omitempty"`
}

type ApplyDesireStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty" firestore:"conditions,omitempty"`
}
