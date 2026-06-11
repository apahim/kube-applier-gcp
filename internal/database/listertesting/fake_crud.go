package listertesting

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/openshift/kube-applier-gcp/internal/database"
)

// Desire is the type constraint for the generic FakeCRUD. It requires
// FirestoreMetadata access (for reading/writing DocumentID, UpdateTime,
// CreateTime) and DeepCopy (for returning isolated copies from the store).
type Desire[T any] interface {
	*T
	database.FirestoreMetadataAccessor
	DeepCopy() *T
}

// FakeCRUD is a generic in-memory implementation of database.ResourceCRUD[T]
// for unit testing. It tracks UpdateTime per document for optimistic concurrency
// and returns deep copies so that callers cannot mutate the store.
//
// UpdateTime values are deterministic: time.Unix(0, seq) with a monotonic
// counter, so tests do not depend on wall-clock time.
type FakeCRUD[T any, PT Desire[T]] struct {
	mu    sync.Mutex
	store map[string]*T
	seq   int64
}

// NewFakeCRUD returns an empty FakeCRUD ready for use.
func NewFakeCRUD[T any, PT Desire[T]]() *FakeCRUD[T, PT] {
	return &FakeCRUD[T, PT]{store: make(map[string]*T)}
}

func (f *FakeCRUD[T, PT]) nextTime() time.Time {
	f.seq++
	return time.Unix(0, f.seq)
}

func (f *FakeCRUD[T, PT]) clone(obj *T) *T {
	return PT(obj).DeepCopy()
}

func (f *FakeCRUD[T, PT]) Get(_ context.Context, documentID string) (*T, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored, ok := f.store[documentID]
	if !ok {
		return nil, database.NewNotFoundError()
	}
	return f.clone(stored), nil
}

func (f *FakeCRUD[T, PT]) List(_ context.Context) ([]*T, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := make([]string, 0, len(f.store))
	for k := range f.store {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	result := make([]*T, 0, len(f.store))
	for _, k := range keys {
		result = append(result, f.clone(f.store[k]))
	}
	return result, nil
}

func (f *FakeCRUD[T, PT]) Create(_ context.Context, obj *T) (*T, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pt := PT(obj)
	docID := pt.GetDocumentID()
	if docID == "" {
		return nil, fmt.Errorf("FakeCRUD.Create: DocumentID is empty")
	}
	if _, exists := f.store[docID]; exists {
		return nil, status.Errorf(codes.AlreadyExists, "document %s already exists", docID)
	}
	stored := f.clone(obj)
	sp := PT(stored)
	now := f.nextTime()
	sp.SetUpdateTime(now)
	sp.SetCreateTime(now)
	f.store[docID] = stored
	return f.clone(stored), nil
}

func (f *FakeCRUD[T, PT]) Replace(_ context.Context, obj *T) (*T, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pt := PT(obj)
	docID := pt.GetDocumentID()
	stored, ok := f.store[docID]
	if !ok {
		return nil, database.NewNotFoundError()
	}
	storedPT := PT(stored)
	if !pt.GetUpdateTime().Equal(storedPT.GetUpdateTime()) {
		return nil, database.NewPreconditionFailedError()
	}
	replacement := f.clone(obj)
	rp := PT(replacement)
	rp.SetUpdateTime(f.nextTime())
	rp.SetCreateTime(storedPT.GetCreateTime())
	f.store[docID] = replacement
	return f.clone(replacement), nil
}

func (f *FakeCRUD[T, PT]) Delete(_ context.Context, documentID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.store[documentID]; !ok {
		return database.NewNotFoundError()
	}
	delete(f.store, documentID)
	return nil
}
