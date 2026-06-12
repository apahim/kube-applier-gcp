package informers

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	kubeapplier "github.com/openshift/kube-applier-gcp/internal/api/kubeapplier"
	"github.com/openshift/kube-applier-gcp/internal/database"
	"github.com/openshift/kube-applier-gcp/internal/database/listers"
)

// --- Unit tests (no emulator required) ---

func TestListWatchWithoutWatchListSemantics(t *testing.T) {
	lw := listWatchWithoutWatchListSemantics{&cache.ListWatch{}}
	if !lw.IsWatchListSemanticsUnSupported() {
		t.Error("expected IsWatchListSemanticsUnSupported to return true")
	}
}

func TestListerListFromPopulatedCache(t *testing.T) {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})

	desires := []*kubeapplier.ApplyDesire{
		{FirestoreMetadata: kubeapplier.FirestoreMetadata{DocumentID: "c1--a"}},
		{FirestoreMetadata: kubeapplier.FirestoreMetadata{DocumentID: "c1--b"}},
		{FirestoreMetadata: kubeapplier.FirestoreMetadata{DocumentID: "c2--a"}},
	}
	for _, d := range desires {
		if err := indexer.Add(d); err != nil {
			t.Fatalf("indexer.Add: %v", err)
		}
	}

	lister := listers.NewApplyDesireLister(indexer)

	items, err := lister.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("List returned %d items, want 3", len(items))
	}
}

func TestListerGetFromPopulatedCache(t *testing.T) {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})

	d := &kubeapplier.ApplyDesire{
		FirestoreMetadata: kubeapplier.FirestoreMetadata{DocumentID: "c1--a"},
		Spec: kubeapplier.ApplyDesireSpec{ClusterID: "c1"},
	}
	if err := indexer.Add(d); err != nil {
		t.Fatalf("indexer.Add: %v", err)
	}

	lister := listers.NewApplyDesireLister(indexer)

	got, err := lister.Get("c1--a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Spec.ClusterID != "c1" {
		t.Errorf("ClusterID = %q, want %q", got.Spec.ClusterID, "c1")
	}
}

func TestListerGetNotFound(t *testing.T) {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	lister := listers.NewApplyDesireLister(indexer)

	_, err := lister.Get("nonexistent")
	if !database.IsNotFoundError(err) {
		t.Errorf("expected NotFoundError, got %v", err)
	}
}

func TestDeleteDesireListerFromPopulatedCache(t *testing.T) {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	d := &kubeapplier.DeleteDesire{
		FirestoreMetadata: kubeapplier.FirestoreMetadata{DocumentID: "c1--del1"},
		Spec:              kubeapplier.DeleteDesireSpec{ClusterID: "c1"},
	}
	if err := indexer.Add(d); err != nil {
		t.Fatalf("indexer.Add: %v", err)
	}

	lister := listers.NewDeleteDesireLister(indexer)

	got, err := lister.Get("c1--del1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Spec.ClusterID != "c1" {
		t.Errorf("ClusterID = %q, want %q", got.Spec.ClusterID, "c1")
	}

	items, err := lister.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("List returned %d items, want 1", len(items))
	}
}

func TestReadDesireListerFromPopulatedCache(t *testing.T) {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	d := &kubeapplier.ReadDesire{
		FirestoreMetadata: kubeapplier.FirestoreMetadata{DocumentID: "c1--read1"},
		Spec:              kubeapplier.ReadDesireSpec{ClusterID: "c1"},
	}
	if err := indexer.Add(d); err != nil {
		t.Fatalf("indexer.Add: %v", err)
	}

	lister := listers.NewReadDesireLister(indexer)

	got, err := lister.Get("c1--read1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Spec.ClusterID != "c1" {
		t.Errorf("ClusterID = %q, want %q", got.Spec.ClusterID, "c1")
	}

	items, err := lister.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("List returned %d items, want 1", len(items))
	}
}

// --- Integration tests (require FIRESTORE_EMULATOR_HOST) ---

func requireEmulator(t *testing.T) {
	t.Helper()
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set; skipping integration test")
	}
}

func newTestClient(t *testing.T) *firestore.Client {
	t.Helper()
	ctx := context.Background()
	dbName := fmt.Sprintf("mc-test-%d", time.Now().UnixNano())
	client, err := firestore.NewClientWithDatabase(ctx, "test-project", dbName)
	if err != nil {
		t.Fatalf("firestore.NewClientWithDatabase: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func startAndSync(t *testing.T, ctx context.Context, info KubeApplierInformers) {
	t.Helper()
	go info.RunWithContext(ctx)
	applyInf, _ := info.ApplyDesires()
	deleteInf, _ := info.DeleteDesires()
	readInf, _ := info.ReadDesires()
	if !cache.WaitForCacheSync(ctx.Done(), applyInf.HasSynced, deleteInf.HasSynced, readInf.HasSynced) {
		t.Fatal("informers did not sync")
	}
}

func waitForCacheCount(t *testing.T, store cache.Store, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		if len(store.List()) == want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for cache to contain %d items (has %d)", want, len(store.List()))
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func TestIntegration_InformerSyncsExistingDocuments(t *testing.T) {
	requireEmulator(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := newTestClient(t)
	dbClient := database.NewFirestoreKubeApplierDBClient(client, client)

	for i := 0; i < 3; i++ {
		d := &kubeapplier.ApplyDesire{
			FirestoreMetadata: kubeapplier.FirestoreMetadata{DocumentID: fmt.Sprintf("c1--item%d", i)},
			Spec: kubeapplier.ApplyDesireSpec{
				ManagementCluster: "mc-test",
				ClusterID:         "c1",
				TargetItem: kubeapplier.ResourceReference{
					Version:  "v1",
					Resource: "configmaps",
					Name:     fmt.Sprintf("cm-%d", i),
				},
			},
		}
		if _, err := dbClient.ApplyDesireStatus().Create(ctx, d); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}

	info := NewKubeApplierInformersWithResyncPeriod(client, 30*time.Second)
	startAndSync(t, ctx, info)

	applyInf, applyLister := info.ApplyDesires()
	if len(applyInf.GetStore().List()) != 3 {
		t.Errorf("expected 3 items in cache, got %d", len(applyInf.GetStore().List()))
	}

	items, err := applyLister.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("lister returned %d items, want 3", len(items))
	}

	got, err := applyLister.Get("c1--item1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Spec.ClusterID != "c1" {
		t.Errorf("ClusterID = %q, want %q", got.Spec.ClusterID, "c1")
	}
}

func TestIntegration_ListenerDeliversEvents(t *testing.T) {
	requireEmulator(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := newTestClient(t)
	dbClient := database.NewFirestoreKubeApplierDBClient(client, client)
	crud := dbClient.ApplyDesireStatus()

	info := NewKubeApplierInformersWithResyncPeriod(client, 30*time.Second)
	startAndSync(t, ctx, info)

	applyInf, _ := info.ApplyDesires()

	// Create a document — the listener should deliver it.
	d := &kubeapplier.ApplyDesire{
		FirestoreMetadata: kubeapplier.FirestoreMetadata{DocumentID: "c1--live"},
		Spec: kubeapplier.ApplyDesireSpec{
			ManagementCluster: "mc-test",
			ClusterID:         "c1",
			TargetItem: kubeapplier.ResourceReference{
				Version:  "v1",
				Resource: "configmaps",
				Name:     "live-cm",
			},
			KubeContent: &runtime.RawExtension{
				Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap"}`),
			},
		},
	}
	created, err := crud.Create(ctx, d)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitForCacheCount(t, applyInf.GetStore(), 1, 10*time.Second)

	// Modify the document.
	created.Spec.ClusterID = "c2"
	if _, err := crud.Replace(ctx, created); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	// Wait for the modification to appear.
	deadline := time.After(10 * time.Second)
	for {
		item, exists, _ := applyInf.GetStore().GetByKey("c1--live")
		if exists {
			if item.(*kubeapplier.ApplyDesire).Spec.ClusterID == "c2" {
				break
			}
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for modification in cache")
		case <-time.After(50 * time.Millisecond):
		}
	}

	// Delete the document.
	if err := crud.Delete(ctx, "c1--live"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	waitForCacheCount(t, applyInf.GetStore(), 0, 10*time.Second)
}

func TestIntegration_PerDatabaseIsolation(t *testing.T) {
	requireEmulator(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clientA := newTestClient(t)
	clientB := newTestClient(t)

	dbClientA := database.NewFirestoreKubeApplierDBClient(clientA, clientA)

	infoA := NewKubeApplierInformersWithResyncPeriod(clientA, 30*time.Second)
	infoB := NewKubeApplierInformersWithResyncPeriod(clientB, 30*time.Second)
	startAndSync(t, ctx, infoA)
	startAndSync(t, ctx, infoB)

	applyInfA, _ := infoA.ApplyDesires()
	applyInfB, _ := infoB.ApplyDesires()

	d := &kubeapplier.ApplyDesire{
		FirestoreMetadata: kubeapplier.FirestoreMetadata{DocumentID: "c1--isolated"},
		Spec: kubeapplier.ApplyDesireSpec{
			ManagementCluster: "mc-a",
			ClusterID:         "c1",
			TargetItem: kubeapplier.ResourceReference{
				Version:  "v1",
				Resource: "configmaps",
				Name:     "iso-cm",
			},
		},
	}
	if _, err := dbClientA.ApplyDesireStatus().Create(ctx, d); err != nil {
		t.Fatalf("Create in A: %v", err)
	}

	waitForCacheCount(t, applyInfA.GetStore(), 1, 10*time.Second)

	// B should remain empty.
	time.Sleep(500 * time.Millisecond)
	if len(applyInfB.GetStore().List()) != 0 {
		t.Errorf("expected 0 items in B's cache, got %d", len(applyInfB.GetStore().List()))
	}
}

func TestIntegration_AllThreeInformerTypes(t *testing.T) {
	requireEmulator(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := newTestClient(t)
	dbClient := database.NewFirestoreKubeApplierDBClient(client, client)

	ad := &kubeapplier.ApplyDesire{
		FirestoreMetadata: kubeapplier.FirestoreMetadata{DocumentID: "c1--apply"},
		Spec: kubeapplier.ApplyDesireSpec{
			ManagementCluster: "mc-test",
			ClusterID:         "c1",
			TargetItem:        kubeapplier.ResourceReference{Version: "v1", Resource: "configmaps", Name: "cm"},
		},
	}
	dd := &kubeapplier.DeleteDesire{
		FirestoreMetadata: kubeapplier.FirestoreMetadata{DocumentID: "c1--delete"},
		Spec: kubeapplier.DeleteDesireSpec{
			ManagementCluster: "mc-test",
			ClusterID:         "c1",
			TargetItem:        kubeapplier.ResourceReference{Version: "v1", Resource: "configmaps", Name: "cm"},
		},
	}
	rd := &kubeapplier.ReadDesire{
		FirestoreMetadata: kubeapplier.FirestoreMetadata{DocumentID: "c1--read"},
		Spec: kubeapplier.ReadDesireSpec{
			ManagementCluster: "mc-test",
			ClusterID:         "c1",
			TargetItem:        kubeapplier.ResourceReference{Version: "v1", Resource: "configmaps", Name: "cm"},
		},
		Status: kubeapplier.ReadDesireStatus{
			KubeContent: &runtime.RawExtension{Raw: []byte(`{"data":"test"}`)},
			Conditions: []metav1.Condition{
				{
					Type:               kubeapplier.ConditionTypeSuccessful,
					Status:             metav1.ConditionTrue,
					Reason:             kubeapplier.ConditionReasonNoErrors,
					LastTransitionTime: metav1.NewTime(time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)),
				},
			},
		},
	}

	if _, err := dbClient.ApplyDesireStatus().Create(ctx, ad); err != nil {
		t.Fatalf("Create ApplyDesire: %v", err)
	}
	if _, err := dbClient.DeleteDesireStatus().Create(ctx, dd); err != nil {
		t.Fatalf("Create DeleteDesire: %v", err)
	}
	if _, err := dbClient.ReadDesireStatus().Create(ctx, rd); err != nil {
		t.Fatalf("Create ReadDesire: %v", err)
	}

	info := NewKubeApplierInformersWithResyncPeriod(client, 30*time.Second)
	startAndSync(t, ctx, info)

	applyInf, applyLister := info.ApplyDesires()
	deleteInf, deleteLister := info.DeleteDesires()
	readInf, readLister := info.ReadDesires()

	waitForCacheCount(t, applyInf.GetStore(), 1, 10*time.Second)
	waitForCacheCount(t, deleteInf.GetStore(), 1, 10*time.Second)
	waitForCacheCount(t, readInf.GetStore(), 1, 10*time.Second)

	aItems, err := applyLister.List()
	if err != nil {
		t.Fatalf("ApplyDesireLister.List: %v", err)
	}
	if len(aItems) != 1 || aItems[0].DocumentID != "c1--apply" {
		t.Errorf("unexpected ApplyDesire list: %+v", aItems)
	}

	dItems, err := deleteLister.List()
	if err != nil {
		t.Fatalf("DeleteDesireLister.List: %v", err)
	}
	if len(dItems) != 1 || dItems[0].DocumentID != "c1--delete" {
		t.Errorf("unexpected DeleteDesire list: %+v", dItems)
	}

	rItems, err := readLister.List()
	if err != nil {
		t.Fatalf("ReadDesireLister.List: %v", err)
	}
	if len(rItems) != 1 || rItems[0].DocumentID != "c1--read" {
		t.Errorf("unexpected ReadDesire list: %+v", rItems)
	}

	// Verify ReadDesire KubeContent survived the round-trip.
	rGot, err := readLister.Get("c1--read")
	if err != nil {
		t.Fatalf("ReadDesireLister.Get: %v", err)
	}
	if rGot.Status.KubeContent == nil {
		t.Error("ReadDesire.Status.KubeContent is nil after informer sync")
	}
}
