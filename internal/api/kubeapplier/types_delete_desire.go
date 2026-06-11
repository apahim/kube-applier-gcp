package kubeapplier

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DeleteDesire targets a single Kubernetes object on the management cluster
// for deletion.
type DeleteDesire struct {
	FirestoreMetadata `json:"firestoreMetadata"`
	Spec              DeleteDesireSpec   `json:"spec" firestore:"spec"`
	Status            DeleteDesireStatus `json:"status" firestore:"status"`
}

type DeleteDesireSpec struct {
	ManagementCluster string            `json:"managementCluster" firestore:"managementCluster"`
	ClusterName       string            `json:"clusterName" firestore:"clusterName"`
	NodePoolName      string            `json:"nodePoolName,omitempty" firestore:"nodePoolName,omitempty"`
	TargetItem        ResourceReference `json:"targetItem,omitempty" firestore:"targetItem"`
}

type DeleteDesireStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty" firestore:"conditions,omitempty"`
}
