package database

import (
	"cloud.google.com/go/firestore"

	kubeapplier "github.com/openshift/kube-applier-gcp/internal/api/kubeapplier"
)

const (
	CollectionApplyDesires  = "applydesires"
	CollectionDeleteDesires = "deletedesires"
	CollectionReadDesires   = "readdesires"
)

type firestoreKubeApplierDBClient struct {
	client *firestore.Client
}

// NewFirestoreKubeApplierDBClient returns a KubeApplierDBClient backed by the
// given Firestore client. The client should be scoped to a single management
// cluster's named database (e.g., "mc-{clusterName}").
func NewFirestoreKubeApplierDBClient(client *firestore.Client) KubeApplierDBClient {
	return &firestoreKubeApplierDBClient{client: client}
}

func (c *firestoreKubeApplierDBClient) ApplyDesires() ResourceCRUD[kubeapplier.ApplyDesire] {
	return &firestoreDesireCRUD[kubeapplier.ApplyDesire, *kubeapplier.ApplyDesire]{
		client:     c.client,
		collection: CollectionApplyDesires,
	}
}

func (c *firestoreKubeApplierDBClient) DeleteDesires() ResourceCRUD[kubeapplier.DeleteDesire] {
	return &firestoreDesireCRUD[kubeapplier.DeleteDesire, *kubeapplier.DeleteDesire]{
		client:     c.client,
		collection: CollectionDeleteDesires,
	}
}

func (c *firestoreKubeApplierDBClient) ReadDesires() ResourceCRUD[kubeapplier.ReadDesire] {
	return &firestoreDesireCRUD[kubeapplier.ReadDesire, *kubeapplier.ReadDesire]{
		client:     c.client,
		collection: CollectionReadDesires,
	}
}

func (c *firestoreKubeApplierDBClient) Close() error {
	return c.client.Close()
}
