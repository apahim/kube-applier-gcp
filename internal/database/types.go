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

// SpecReader provides read-only access to spec documents in the specs
// database. The agent uses this to read desire specifications written by the
// backend.
type SpecReader[T any] interface {
	Get(ctx context.Context, documentID string) (*T, error)
	List(ctx context.Context) ([]*T, error)
}

// ResourceCRUD is the generic CRUD interface for a single Firestore collection.
// Used for status documents in the status database where the agent needs full
// read-write access.
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

// KubeApplierDBClient is the per-management-cluster handle that wraps two
// Firestore named databases: specs (read-only for the agent) and status
// (read-write for the agent). IAM enforces directional isolation: the agent
// has datastore.viewer on specs and datastore.user on status.
type KubeApplierDBClient interface {
	ApplyDesireSpecs() SpecReader[kubeapplier.ApplyDesire]
	DeleteDesireSpecs() SpecReader[kubeapplier.DeleteDesire]
	ReadDesireSpecs() SpecReader[kubeapplier.ReadDesire]

	ApplyDesireStatus() ResourceCRUD[kubeapplier.ApplyDesire]
	DeleteDesireStatus() ResourceCRUD[kubeapplier.DeleteDesire]
	ReadDesireStatus() ResourceCRUD[kubeapplier.ReadDesire]

	Close() error
}
