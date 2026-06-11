package listertesting

import (
	kubeapplier "github.com/openshift/kube-applier-gcp/internal/api/kubeapplier"
	"github.com/openshift/kube-applier-gcp/internal/database"
)

// FakeKubeApplierDBClient is an in-memory implementation of
// database.KubeApplierDBClient for unit testing. Each desire collection
// is backed by a FakeCRUD with independent storage and UpdateTime tracking.
type FakeKubeApplierDBClient struct {
	applyDesires  *FakeCRUD[kubeapplier.ApplyDesire, *kubeapplier.ApplyDesire]
	deleteDesires *FakeCRUD[kubeapplier.DeleteDesire, *kubeapplier.DeleteDesire]
	readDesires   *FakeCRUD[kubeapplier.ReadDesire, *kubeapplier.ReadDesire]
}

var _ database.KubeApplierDBClient = (*FakeKubeApplierDBClient)(nil)

// NewFakeKubeApplierDBClient returns a ready-to-use fake client with empty
// collections.
func NewFakeKubeApplierDBClient() *FakeKubeApplierDBClient {
	return &FakeKubeApplierDBClient{
		applyDesires:  NewFakeCRUD[kubeapplier.ApplyDesire, *kubeapplier.ApplyDesire](),
		deleteDesires: NewFakeCRUD[kubeapplier.DeleteDesire, *kubeapplier.DeleteDesire](),
		readDesires:   NewFakeCRUD[kubeapplier.ReadDesire, *kubeapplier.ReadDesire](),
	}
}

func (c *FakeKubeApplierDBClient) ApplyDesires() database.ResourceCRUD[kubeapplier.ApplyDesire] {
	return c.applyDesires
}

func (c *FakeKubeApplierDBClient) DeleteDesires() database.ResourceCRUD[kubeapplier.DeleteDesire] {
	return c.deleteDesires
}

func (c *FakeKubeApplierDBClient) ReadDesires() database.ResourceCRUD[kubeapplier.ReadDesire] {
	return c.readDesires
}

func (c *FakeKubeApplierDBClient) Close() error { return nil }
