package database

import (
	"context"
	"time"

	kubeapplier "github.com/openshift/kube-applier-gcp/internal/api/kubeapplier"
)

// FirestoreMetadataAccessor provides generic access to the server-managed
// metadata fields that every desire type carries via embedded FirestoreMetadata.
type FirestoreMetadataAccessor interface {
	GetDocumentID() string
	GetUpdateTime() time.Time
	GetCreateTime() time.Time
	SetDocumentID(string)
	SetUpdateTime(time.Time)
	SetCreateTime(time.Time)
}

// SpecStatusAccessor provides generic access to the two data fields that every
// desire type stores in Firestore. Replace uses this to build firestore.Update
// entries for the "spec" and "status" paths, since firestore.Set does not
// accept a LastUpdateTime precondition.
type SpecStatusAccessor interface {
	GetSpec() any
	GetStatus() any
}

// ResourceCRUD is the generic CRUD interface for a single Firestore collection.
// Each desire type (ApplyDesire, DeleteDesire, ReadDesire) gets its own instance
// scoped to its collection name.
//
// Get returns NewNotFoundError() when the document doesn't exist.
// Create returns codes.AlreadyExists when the document already exists.
// Replace uses optimistic concurrency via UpdateTime; it returns
// NewPreconditionFailedError() when the document has changed since last read.
type ResourceCRUD[T any] interface {
	Get(ctx context.Context, documentID string) (*T, error)
	List(ctx context.Context) ([]*T, error)
	Create(ctx context.Context, obj *T) (*T, error)
	Replace(ctx context.Context, obj *T) (*T, error)
	Delete(ctx context.Context, documentID string) error
}

// KubeApplierDBClient is the per-database handle for a single management
// cluster's Firestore named database. It provides typed CRUD access to each
// desire collection.
type KubeApplierDBClient interface {
	ApplyDesires() ResourceCRUD[kubeapplier.ApplyDesire]
	DeleteDesires() ResourceCRUD[kubeapplier.DeleteDesire]
	ReadDesires() ResourceCRUD[kubeapplier.ReadDesire]
	Close() error
}
