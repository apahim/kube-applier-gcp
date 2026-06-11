package kubeapplier

// ResourceReference identifies a single Kubernetes object on the management
// cluster. It is shared by every *Desire spec.targetItem (ApplyDesire,
// DeleteDesire, ReadDesire) so the kube-applier resolves the GVR and (if
// applicable) namespace + name without consulting a RESTMapper.
type ResourceReference struct {
	Group     string `json:"group" firestore:"group"`
	Version   string `json:"version" firestore:"version"`
	Resource  string `json:"resource" firestore:"resource"`
	Namespace string `json:"namespace,omitempty" firestore:"namespace,omitempty"`
	Name      string `json:"name" firestore:"name"`
}
