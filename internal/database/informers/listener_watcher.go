package informers

import (
	"context"
	"net/http"

	"cloud.google.com/go/firestore"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
)

type firestoreWatcher struct {
	resultCh chan watch.Event
	done     chan struct{}
	cancel   context.CancelFunc
}

func newFirestoreWatcher(
	ctx context.Context,
	collection *firestore.CollectionRef,
	convertFn func(*firestore.DocumentSnapshot) (runtime.Object, error),
) *firestoreWatcher {
	ctx, cancel := context.WithCancel(ctx)
	w := &firestoreWatcher{
		resultCh: make(chan watch.Event, 100),
		done:     make(chan struct{}),
		cancel:   cancel,
	}
	go w.run(ctx, collection, convertFn)
	return w
}

func (w *firestoreWatcher) run(
	ctx context.Context,
	collection *firestore.CollectionRef,
	convertFn func(*firestore.DocumentSnapshot) (runtime.Object, error),
) {
	defer close(w.done)
	defer close(w.resultCh)

	snapIter := collection.Snapshots(ctx)
	defer snapIter.Stop()

	for {
		snap, err := snapIter.Next()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			w.sendError(ctx, err)
			return
		}
		for _, change := range snap.Changes {
			obj, err := convertFn(change.Doc)
			if err != nil {
				continue
			}
			var eventType watch.EventType
			switch change.Kind {
			case firestore.DocumentAdded:
				eventType = watch.Added
			case firestore.DocumentModified:
				eventType = watch.Modified
			case firestore.DocumentRemoved:
				eventType = watch.Deleted
			}
			select {
			case w.resultCh <- watch.Event{Type: eventType, Object: obj}:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (w *firestoreWatcher) sendError(ctx context.Context, err error) {
	event := watch.Event{
		Type: watch.Error,
		Object: &metav1.Status{
			Status:  metav1.StatusFailure,
			Code:    http.StatusGone,
			Reason:  metav1.StatusReasonExpired,
			Message: err.Error(),
		},
	}
	select {
	case w.resultCh <- event:
	case <-ctx.Done():
	}
}

func (w *firestoreWatcher) Stop() {
	w.cancel()
}

func (w *firestoreWatcher) ResultChan() <-chan watch.Event {
	return w.resultCh
}
