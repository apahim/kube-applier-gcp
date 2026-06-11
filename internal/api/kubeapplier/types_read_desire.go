package kubeapplier

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// ReadDesire indicates a kube item in .spec.targetItem to issue a
// list/watch+informer for, mirroring the live object into .status.kubeContent.
type ReadDesire struct {
	FirestoreMetadata `json:"firestoreMetadata"`
	Spec              ReadDesireSpec   `json:"spec" firestore:"spec"`
	Status            ReadDesireStatus `json:"status" firestore:"status"`
}

type ReadDesireSpec struct {
	ManagementCluster string            `json:"managementCluster" firestore:"managementCluster"`
	ClusterName       string            `json:"clusterName" firestore:"clusterName"`
	NodePoolName      string            `json:"nodePoolName,omitempty" firestore:"nodePoolName,omitempty"`
	TargetItem        ResourceReference `json:"targetItem,omitempty" firestore:"targetItem"`
}

type ReadDesireStatus struct {
	Conditions  []metav1.Condition    `json:"conditions,omitempty" firestore:"conditions,omitempty"`
	KubeContent *runtime.RawExtension `json:"kubeContent,omitempty" firestore:"kubeContent,omitempty"`
}
