// Package desirestatuswriter is the generic "read-mutate-replace" helper that
// writes back the .status section of kube-applier *Desire Firestore documents
// (ApplyDesire, DeleteDesire, ReadDesire).
//
// The kube-applier never creates desires (the backend does), so the helper
// deliberately omits the create-if-missing branch.
//
// Callers supply two collaborators as named structs implementing Fetcher
// and Replacer. Fetcher implementations MUST go to a live Firestore client
// rather than a cached lister: the Replacer's UpdateTime precondition check
// needs the freshest revision available.
package desirestatuswriter

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"

	"github.com/openshift/kube-applier-gcp/internal/database"
)

func init() {
	// FirestoreMetadata embeds time.Time which has unexported fields.
	// equality.Semantic.DeepEqual panics on unexported fields unless a
	// custom comparator is registered.
	if err := equality.Semantic.AddFunc(func(a, b time.Time) bool {
		return a.Equal(b)
	}); err != nil {
		panic(err)
	}
}

// Fetcher reads the current state of a single desire by a controller-defined
// typed key.
type Fetcher[T any, K comparable] interface {
	Fetch(ctx context.Context, key K) (*T, error)
}

// Replacer writes back a fully-populated desire.
type Replacer[T any] interface {
	Replace(ctx context.Context, desired *T) error
}

// DeepCopyable is the constraint on the pointer-type parameter that lets the
// StatusWriter clone the value it receives from the Fetcher without knowing
// T's concrete shape.
type DeepCopyable[T any] interface {
	*T
	DeepCopy() *T
}

// MutateFunc deep-mutates a desire to record the latest controller observation.
// It must not perform IO; precompute everything first.
type MutateFunc[T any] func(*T)

// StatusWriter computes the next desired state via mutate and writes it back
// once. It returns nil for a no-op (status already up-to-date) and for an
// UpdateTime conflict (the informer will requeue when the new revision lands).
type StatusWriter[T any, K comparable] interface {
	UpdateStatus(ctx context.Context, key K, mutate MutateFunc[T]) error
}

// New returns a StatusWriter that fetches via fetcher and writes via replacer.
func New[T any, K comparable, PT DeepCopyable[T]](
	fetcher Fetcher[T, K], replacer Replacer[T],
) StatusWriter[T, K] {
	return &writer[T, K, PT]{fetcher: fetcher, replacer: replacer}
}

type writer[T any, K comparable, PT DeepCopyable[T]] struct {
	fetcher  Fetcher[T, K]
	replacer Replacer[T]
}

func (w *writer[T, K, PT]) UpdateStatus(ctx context.Context, key K, mutate MutateFunc[T]) error {
	existing, err := w.fetcher.Fetch(ctx, key)
	if err != nil {
		if database.IsNotFoundError(err) {
			return nil
		}
		return fmt.Errorf("fetch %v: %w", key, err)
	}
	if existing == nil {
		return nil
	}

	desired := PT(existing).DeepCopy()
	mutate(desired)

	if equality.Semantic.DeepEqual(existing, desired) {
		return nil
	}

	if err := w.replacer.Replace(ctx, desired); err != nil {
		return fmt.Errorf("replace status for %v: %w", key, err)
	}
	return nil
}
