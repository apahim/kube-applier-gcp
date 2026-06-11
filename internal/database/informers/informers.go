package informers

import (
	"context"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"

	kubeapplier "github.com/openshift/kube-applier-gcp/internal/api/kubeapplier"
	"github.com/openshift/kube-applier-gcp/internal/database"
	"github.com/openshift/kube-applier-gcp/internal/database/listers"
)

const defaultResyncPeriod = 30 * time.Second

type KubeApplierInformers interface {
	ApplyDesires() (cache.SharedIndexInformer, listers.ApplyDesireLister)
	DeleteDesires() (cache.SharedIndexInformer, listers.DeleteDesireLister)
	ReadDesires() (cache.SharedIndexInformer, listers.ReadDesireLister)
	RunWithContext(ctx context.Context)
}

type kubeApplierInformers struct {
	applyDesireInformer  cache.SharedIndexInformer
	applyDesireLister    listers.ApplyDesireLister
	deleteDesireInformer cache.SharedIndexInformer
	deleteDesireLister   listers.DeleteDesireLister
	readDesireInformer   cache.SharedIndexInformer
	readDesireLister     listers.ReadDesireLister
}

func NewKubeApplierInformers(client *firestore.Client) KubeApplierInformers {
	return NewKubeApplierInformersWithResyncPeriod(client, defaultResyncPeriod)
}

func NewKubeApplierInformersWithResyncPeriod(client *firestore.Client, resyncPeriod time.Duration) KubeApplierInformers {
	applyInf := newDesireInformer(
		client.Collection(database.CollectionApplyDesires),
		&kubeapplier.ApplyDesire{},
		func(snap *firestore.DocumentSnapshot) (runtime.Object, error) {
			return database.SnapshotToApplyDesire(snap)
		},
		func(items []*firestore.DocumentSnapshot) (runtime.Object, error) {
			list := &kubeapplier.ApplyDesireList{}
			list.ResourceVersion = "0"
			for _, snap := range items {
				d, err := database.SnapshotToApplyDesire(snap)
				if err != nil {
					return nil, err
				}
				list.Items = append(list.Items, *d)
			}
			return list, nil
		},
		resyncPeriod,
	)
	deleteInf := newDesireInformer(
		client.Collection(database.CollectionDeleteDesires),
		&kubeapplier.DeleteDesire{},
		func(snap *firestore.DocumentSnapshot) (runtime.Object, error) {
			return database.SnapshotToDeleteDesire(snap)
		},
		func(items []*firestore.DocumentSnapshot) (runtime.Object, error) {
			list := &kubeapplier.DeleteDesireList{}
			list.ResourceVersion = "0"
			for _, snap := range items {
				d, err := database.SnapshotToDeleteDesire(snap)
				if err != nil {
					return nil, err
				}
				list.Items = append(list.Items, *d)
			}
			return list, nil
		},
		resyncPeriod,
	)
	readInf := newDesireInformer(
		client.Collection(database.CollectionReadDesires),
		&kubeapplier.ReadDesire{},
		func(snap *firestore.DocumentSnapshot) (runtime.Object, error) {
			return database.SnapshotToReadDesire(snap)
		},
		func(items []*firestore.DocumentSnapshot) (runtime.Object, error) {
			list := &kubeapplier.ReadDesireList{}
			list.ResourceVersion = "0"
			for _, snap := range items {
				d, err := database.SnapshotToReadDesire(snap)
				if err != nil {
					return nil, err
				}
				list.Items = append(list.Items, *d)
			}
			return list, nil
		},
		resyncPeriod,
	)
	return &kubeApplierInformers{
		applyDesireInformer:  applyInf,
		applyDesireLister:    listers.NewApplyDesireLister(applyInf.GetIndexer()),
		deleteDesireInformer: deleteInf,
		deleteDesireLister:   listers.NewDeleteDesireLister(deleteInf.GetIndexer()),
		readDesireInformer:   readInf,
		readDesireLister:     listers.NewReadDesireLister(readInf.GetIndexer()),
	}
}

func newDesireInformer(
	collection *firestore.CollectionRef,
	exampleObj runtime.Object,
	convertFn func(*firestore.DocumentSnapshot) (runtime.Object, error),
	listFn func([]*firestore.DocumentSnapshot) (runtime.Object, error),
	resyncPeriod time.Duration,
) cache.SharedIndexInformer {
	lw := &cache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, _ metav1.ListOptions) (runtime.Object, error) {
			snaps, err := collection.Documents(ctx).GetAll()
			if err != nil {
				return nil, err
			}
			return listFn(snaps)
		},
		WatchFuncWithContext: func(ctx context.Context, _ metav1.ListOptions) (watch.Interface, error) {
			return newFirestoreWatcher(ctx, collection, convertFn), nil
		},
	}
	return cache.NewSharedIndexInformerWithOptions(
		&listWatchWithoutWatchListSemantics{lw},
		exampleObj,
		cache.SharedIndexInformerOptions{
			ResyncPeriod: resyncPeriod,
		},
	)
}

func (k *kubeApplierInformers) ApplyDesires() (cache.SharedIndexInformer, listers.ApplyDesireLister) {
	return k.applyDesireInformer, k.applyDesireLister
}

func (k *kubeApplierInformers) DeleteDesires() (cache.SharedIndexInformer, listers.DeleteDesireLister) {
	return k.deleteDesireInformer, k.deleteDesireLister
}

func (k *kubeApplierInformers) ReadDesires() (cache.SharedIndexInformer, listers.ReadDesireLister) {
	return k.readDesireInformer, k.readDesireLister
}

func (k *kubeApplierInformers) RunWithContext(ctx context.Context) {
	var wg sync.WaitGroup

	wg.Add(3)
	go func() {
		defer wg.Done()
		k.applyDesireInformer.RunWithContext(ctx)
	}()
	go func() {
		defer wg.Done()
		k.deleteDesireInformer.RunWithContext(ctx)
	}()
	go func() {
		defer wg.Done()
		k.readDesireInformer.RunWithContext(ctx)
	}()

	<-ctx.Done()
	wg.Wait()
}
