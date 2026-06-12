package database

import (
	"errors"

	"cloud.google.com/go/firestore"

	kubeapplier "github.com/openshift/kube-applier-gcp/internal/api/kubeapplier"
)

const (
	CollectionApplyDesires  = "applydesires"
	CollectionDeleteDesires = "deletedesires"
	CollectionReadDesires   = "readdesires"
)

type firestoreKubeApplierDBClient struct {
	specsClient  *firestore.Client
	statusClient *firestore.Client
}

// NewFirestoreKubeApplierDBClient returns a KubeApplierDBClient backed by two
// Firestore named databases: specsClient for read-only spec access and
// statusClient for read-write status access.
func NewFirestoreKubeApplierDBClient(specsClient, statusClient *firestore.Client) KubeApplierDBClient {
	return &firestoreKubeApplierDBClient{specsClient: specsClient, statusClient: statusClient}
}

func (c *firestoreKubeApplierDBClient) ApplyDesireSpecs() SpecReader[kubeapplier.ApplyDesire] {
	return &firestoreSpecReader[kubeapplier.ApplyDesire, *kubeapplier.ApplyDesire]{
		client:     c.specsClient,
		collection: CollectionApplyDesires,
	}
}

func (c *firestoreKubeApplierDBClient) DeleteDesireSpecs() SpecReader[kubeapplier.DeleteDesire] {
	return &firestoreSpecReader[kubeapplier.DeleteDesire, *kubeapplier.DeleteDesire]{
		client:     c.specsClient,
		collection: CollectionDeleteDesires,
	}
}

func (c *firestoreKubeApplierDBClient) ReadDesireSpecs() SpecReader[kubeapplier.ReadDesire] {
	return &firestoreSpecReader[kubeapplier.ReadDesire, *kubeapplier.ReadDesire]{
		client:     c.specsClient,
		collection: CollectionReadDesires,
	}
}

func (c *firestoreKubeApplierDBClient) ApplyDesireStatus() ResourceCRUD[kubeapplier.ApplyDesire] {
	return &firestoreDesireCRUD[kubeapplier.ApplyDesire, *kubeapplier.ApplyDesire]{
		client:     c.statusClient,
		collection: CollectionApplyDesires,
	}
}

func (c *firestoreKubeApplierDBClient) DeleteDesireStatus() ResourceCRUD[kubeapplier.DeleteDesire] {
	return &firestoreDesireCRUD[kubeapplier.DeleteDesire, *kubeapplier.DeleteDesire]{
		client:     c.statusClient,
		collection: CollectionDeleteDesires,
	}
}

func (c *firestoreKubeApplierDBClient) ReadDesireStatus() ResourceCRUD[kubeapplier.ReadDesire] {
	return &firestoreDesireCRUD[kubeapplier.ReadDesire, *kubeapplier.ReadDesire]{
		client:     c.statusClient,
		collection: CollectionReadDesires,
	}
}

func (c *firestoreKubeApplierDBClient) Close() error {
	return errors.Join(c.specsClient.Close(), c.statusClient.Close())
}
