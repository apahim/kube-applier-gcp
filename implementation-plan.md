# GCP HCP Kube-Applier Implementation Plan

## Context

The ARO HCP kube-applier (`/Users/asegundo/git-gcp/ARO-HCP/kube-applier`) is a per-management-cluster controller binary that brokers between a document database (Azure Cosmos DB) and the local Kubernetes apiserver. It reconciles three "Desire" document types: **ApplyDesire** (SSA apply), **DeleteDesire** (delete + wait for finalizers), **ReadDesire** (informer-backed read mirror). A backend service creates these documents; the kube-applier only reads specs and writes status.

This plan adapts the kube-applier for GCP HCP, replacing Cosmos DB with **Google Cloud Firestore (Native Mode)** as the database backend. Firestore is chosen over Cloud SQL PostgreSQL (which the rest of the GCP HCP ecosystem uses) because it is the best tool for this specific workload: native document model, real-time snapshot listeners, per-database isolation, serverless scaling, and optimistic concurrency — all of which map directly to the Cosmos DB features the kube-applier depends on.

**Shared prompt constraint:** The ARO HCP readme is the canonical shared design prompt. GCP adaptations (database, auth) are product-specific; any changes to the core Desire API structure or controller patterns must be contributed back upstream.

---

## Database Choice: Firestore (Native Mode)

### Why Firestore over Cloud SQL PostgreSQL

| Requirement | Firestore | Cloud SQL PostgreSQL |
|---|---|---|
| Document model (JSON desires) | Native | JSONB (works but impedance mismatch) |
| Per-MC isolation | Named databases (up to 100/project) | Per-schema or per-database |
| Real-time change detection | Snapshot listeners (native streaming) | LISTEN/NOTIFY or polling |
| Optimistic concurrency | `LastUpdateTime` precondition | Manual version column |
| Serverless (no instance sizing) | Yes | No (instance-based billing) |
| Cost at ~10k docs | ~$5-15/month (pay-per-op) | ~$10-30/month minimum (instance) |

### Cosmos DB -> Firestore Mapping

| Cosmos DB | Firestore | Notes |
|---|---|---|
| Database | Project | Project-level grouping |
| Container (per MC) | Named database (per MC) | `mc-{clusterName}` |
| Partition key | N/A | Per-database isolation eliminates partition keys |
| Document etag | `UpdateTime` | Used via `firestore.LastUpdateTime()` precondition |
| ResourceID (ARM path) | Document ID (flat name) | Cluster/nodepool identity moves to spec fields |
| GlobalLister | Collection query per database | Backend iterates databases |
| Change feed / expiringWatcher | Snapshot listener (`Snapshots()`) | True streaming, no artificial relist |

### Firestore Collection Structure (per MC database)

```
database: mc-{managementClusterName}
  applydesires/{clusterID}--{desireName}  -> {spec: {targetItem, kubeContent, managementCluster, clusterID, nodePoolName?}, status: {conditions}}
  deletedesires/{clusterID}--{desireName} -> {spec: {targetItem, managementCluster, clusterID, nodePoolName?}, status: {conditions}}
  readdesires/{clusterID}--{desireName}   -> {spec: {targetItem, managementCluster, clusterID, nodePoolName?}, status: {conditions, kubeContent}}
```

### Authentication & Isolation

- GKE Workload Identity Federation: pod KSA -> IAM GSA (no service account keys)
- Per-database IAM condition: `resource.name == "projects/{project}/databases/mc-{cluster}"`
- Role: `roles/datastore.user` (read + write)
- Equivalent to Cosmos per-container credential scoping

---

## Project Structure

```
kube-applier-gcp/
  go.mod                           # standalone module
  main.go
  Makefile
  Dockerfile
  readme.md                        # (existing shared design prompt)

  cmd/
    root.go                        # GCP-specific flags

  internal/
    api/kubeapplier/
      types_firestoredata.go       # FirestoreMetadata (replaces CosmosMetadata)
      types_apply_desire.go        # ApplyDesire with GCP fields
      types_delete_desire.go       # DeleteDesire with GCP fields
      types_read_desire.go         # ReadDesire with GCP fields
      types_resource_reference.go  # ResourceReference (port verbatim)
      conditions.go                # Condition constants (port verbatim)

    database/
      client.go                    # KubeApplierDBClient interface + Firestore impl
      crud.go                      # FirestoreDesireCRUD[T] with optimistic concurrency
      errors.go                    # IsNotFoundError, IsPreconditionFailedError (gRPC codes)
      informers/
        informers.go               # SharedIndexInformer factory using Firestore listeners
        listener_watcher.go        # Firestore Snapshots() -> watch.Interface bridge
      listers/                     # Indexer-backed listers (indexes: ByCluster, ByNodePool)
      listertesting/               # In-memory fakes with UpdateTime tracking

    controllerutils/
      cooldown.go                  # TimeBasedCooldownChecker (port verbatim)

  pkg/
    app/
      options.go                   # Options struct (ManagementCluster is string, not ARM ResourceID)
      kube_applier.go              # Run loop, leader election, healthz/metrics
      firestore_wiring.go          # Firestore client construction (replaces cosmos_wiring.go)
      kube_wiring.go               # Kubeconfig, dynamic client (port verbatim)
      leader_election_wiring.go    # Leader election lock (port verbatim)

    controllers/
      apply_desire/controller.go   # SSA controller (adapted: UpdateTime replaces etag)
      delete_desire/controller.go  # Delete controller (adapted)
      read_desire_manager/controller.go  # Manager controller (adapted)
      read_desire_kubernetes/controller.go  # Per-instance kube watcher (adapted)
      conditions/conditions.go     # SetSuccessful, SetDegraded, PreCheckError (port verbatim)
      desirestatuswriter/          # Generic status writer (port verbatim)
      keys/keys.go                 # Simplified key type (no ARM ResourceID parsing)

  deploy/                          # Helm chart
  terraform/                       # Firestore DBs + IAM + Workload Identity
```

**Key Go dependencies** (replacing Azure SDK):
- `cloud.google.com/go/firestore` -- Firestore client (Phase 2a+)
- `google.golang.org/grpc v1.81.1` -- gRPC error handling (codes, status)
- `k8s.io/apimachinery v0.36.1`, `k8s.io/client-go v0.36.1`, `k8s.io/utils`, `k8s.io/component-base v0.36.1` -- unchanged
- `github.com/spf13/cobra`, `github.com/prometheus/client_golang`, `github.com/go-logr/logr` -- unchanged
- Go version: `go 1.26.0`

---

## What Ports Verbatim vs What Changes

### Port verbatim (import paths only)

| File (ARO HCP source) | GCP location | Why no changes |
|---|---|---|
| `kube-applier/pkg/controllers/conditions/conditions.go` | `pkg/controllers/conditions/` | Zero Azure dependency |
| `kube-applier/pkg/controllers/desirestatuswriter/` | `pkg/controllers/desirestatuswriter/` | Generic over T; only calls `IsNotFoundError` |
| `internal/controllerutils/cooldown.go` | `internal/controllerutils/` | Pure time-based logic |
| `kube-applier/pkg/app/kube_wiring.go` | `pkg/app/kube_wiring.go` | Standard k8s client construction |
| `kube-applier/pkg/app/leader_election_wiring.go` | `pkg/app/leader_election_wiring.go` | Standard leader election |
| `internal/api/kubeapplier/conditions.go` | `internal/api/kubeapplier/conditions.go` | Condition type/reason constants |
| `internal/api/kubeapplier/types_resource_reference.go` | `internal/api/kubeapplier/` | GVR + name + namespace |

### Changes required

| Component | What changes | Why |
|---|---|---|
| **FirestoreMetadata** (replaces CosmosMetadata) | `DocumentID string`, `UpdateTime time.Time`, `CreateTime time.Time` | Firestore server fields instead of Cosmos etag/resourceID |
| **Desire types** | `ManagementCluster` is `string` (not `*azcorearm.ResourceID`); add `ClusterName`, `NodePoolName` as explicit spec fields | ARM hierarchy flattened |
| **Keys** | `ApplyDesireKey{ClusterID, NodePoolName, Name}` parsed from spec fields, not ARM ResourceID | No ARM parsing needed |
| **Database CRUD** | `firestore.Client` + `Collection().Doc().Get/Set/Delete` with `LastUpdateTime` precondition | Replaces Cosmos container + partition key |
| **Informers** | Firestore `Snapshots()` listener feeds `cache.SharedIndexInformer` via custom `watch.Interface` | Replaces Cosmos expiringWatcher |
| **Controller handleUpdate** | `!oldD.UpdateTime.Equal(newD.UpdateTime)` | Replaces `oldD.GetEtag() != newD.GetEtag()` |
| **Fetcher/Replacer** | Direct flat CRUD (no per-cluster/per-nodepool routing) | Firestore per-database isolation eliminates routing |
| **desirestatuswriter dep** | `database.IsNotFoundError()` must check gRPC `codes.NotFound`; not truly verbatim | Cosmos uses HTTP 404, Firestore uses gRPC codes |
| **CLI flags** | `--firestore-project`, `--firestore-database` | Replaces `--cosmos-url`, `--cosmos-name`, `--cosmos-container` |
| **Client construction** | `firestore.NewClientWithDatabase(ctx, project, database)` | ADC auto-detects Workload Identity |
| **FieldManager** | `"gcp-hcp-kube-applier"` | Replaces `"aro-hcp-kube-applier"` |

### Deliberately eliminated (Cosmos patterns that don't apply to Firestore)

| ARO HCP Pattern | Why it existed | Why we drop it | Replacement |
|---|---|---|---|
| **Per-parent CRUD routing** (`key.CRUD(client)` → `ForCluster()`/`ForNodePool()`) | Cosmos requires a partition key on every query; `ResourceParent` constructs it | Firestore has no partition key; per-MC database isolation makes parent routing unnecessary | Controllers call `client.ApplyDesires().Get(ctx, documentID)` directly |
| **Narrow typed CRUD interfaces** (`KubeApplierApplyDesireCRUD`, etc.) | Wrappers that expose only `ForCluster()`/`ForNodePool()` to each controller | These exist solely for parent-scoped routing, which is eliminated | Controllers take `ResourceCRUD[T]` directly — one interface, pre-scoped to the collection |
| **`UntypedCRUD`** (walks container by resourceID prefix) | Orphan cleanup: delete desires whose parent cluster/nodepool no longer exists | Flat collections + typed lister indexes make typed cleanup simpler | Backend queries by `spec.clusterID` or `spec.nodePoolName` via lister indexes, then batch-deletes through typed `ResourceCRUD[T].Delete()` |
| **`GenericDocument[T]` envelope** | Cosmos wraps each document in a partition-keyed envelope type | Firestore stores documents directly — no envelope, no partition key field | Desires serialize directly via Firestore codec; `FirestoreMetadata` fields use `firestore:"-"` |
| **`KubeApplierDBClients` lister-walk** (plural registry resolves container names via MC lister) | Cosmos container names come from MC status field; must walk lister to resolve | Firestore database names are deterministic: `mc-{clusterName}` — no lookup needed | Direct construction: `NewKubeApplierDBClient(ctx, project, "mc-"+mcName)` |

### Design decisions (Firestore-specific, not a port)

**Named databases vs. subcollections:** This plan uses one Firestore named database per MC (`mc-{clusterName}`), mirroring Cosmos's per-MC container model. The alternative — a single database with subcollections (`clusters/{name}/applydesires/{id}`) — is more idiomatic for Firestore but has worse IAM isolation (path-based rules vs. per-database conditions). Named databases are the right choice because:
- The kube-applier sidecar only opens one database (its own MC), so cross-MC queries aren't needed
- Per-database IAM conditions are simpler and more auditable than path-based rules
- Per-database backup/restore granularity aligns with MC lifecycle
- **Scaling constraint:** Firestore allows up to 100 named databases per project. This is sufficient for the expected MC count.

**Document ID format:** Document IDs are composite: `{clusterID}--{desireName}`. The cluster ID prefix provides structural uniqueness across hosted clusters within the same MC database and makes orphan cleanup by cluster trivial (prefix scan on the ID). Both HostedCluster-scoped and NodePool-scoped desires use the same format — nodepool identity lives in `spec.nodePoolName`, not the document ID.

**Cooldown gates with real-time listeners:** Firestore snapshot listeners deliver events for the controller's own status writes (each write bumps `UpdateTime`, triggering a listener event). Without cooldown gates, this creates the same feedback loop Cosmos has: write status → event fired → controller re-syncs → writes same status → event fired → ... The cooldown gate breaks this cycle by gating unchanged-spec resyncs. This is why cooldown is retained despite Firestore's real-time nature.

**`Update()` for status writes (REVISED):** The original plan proposed `Set()` with `LastUpdateTime()` precondition, but Firestore's Go SDK `Set` only accepts `SetOption` (e.g., `MergeAll`), not `Precondition`. `Update` accepts `Precondition` and replaces the named field paths. Since all data lives under `spec`, `status`, `spec_kubeContent`, and `status_kubeContent`, updating all four paths is equivalent to a full document overwrite. This also avoids the need to serialize the struct to a map for every write — only `Create` needs the map path (to include `firestore:"-"` tagged KubeContent fields).

---

## Key Implementation Details

### 1. FirestoreMetadata (replaces CosmosMetadata)

```go
// internal/api/kubeapplier/types_firestoredata.go
type FirestoreMetadata struct {
    // DocumentID is the Firestore document path relative to the database root.
    // Format: "{clusterID}--{desireName}" (collection is implicit in CRUD scope).
    DocumentID string    `json:"documentID" firestore:"-"`

    // UpdateTime is the Firestore server-managed last-update timestamp.
    // Used as optimistic concurrency token via firestore.LastUpdateTime precondition.
    UpdateTime time.Time `json:"updateTime" firestore:"-"`

    // CreateTime is the Firestore server-managed creation timestamp.
    CreateTime time.Time `json:"createTime,omitempty" firestore:"-"`
}
```

### 2. Desire Types (adapted)

```go
// internal/api/kubeapplier/types_apply_desire.go
type ApplyDesire struct {
    FirestoreMetadata `json:"firestoreMetadata"`
    Spec              ApplyDesireSpec   `json:"spec"`
    Status            ApplyDesireStatus `json:"status"`
}

type ApplyDesireSpec struct {
    ManagementCluster string                `json:"managementCluster" firestore:"managementCluster"`
    ClusterID         string                `json:"clusterID" firestore:"clusterID"`
    NodePoolName      string                `json:"nodePoolName,omitempty" firestore:"nodePoolName,omitempty"`
    TargetItem        ResourceReference     `json:"targetItem" firestore:"targetItem"`
    KubeContent       *runtime.RawExtension `json:"kubeContent,omitempty" firestore:"-"`
}

type ApplyDesireStatus struct {
    Conditions []metav1.Condition `json:"conditions,omitempty" firestore:"conditions,omitempty"`
}
```

Same pattern for DeleteDesire and ReadDesire (ReadDesire adds `KubeContent` in status).
`KubeContent` uses `firestore:"-"` because Firestore's codec rejects `runtime.RawExtension`
(it implements `runtime.Object`). See resolved spike below.

**`runtime.Object` compliance:** All three desire types (plus their List wrappers) must implement `runtime.Object` for the informer cache. Embed `metav1.TypeMeta` with synthetic kind/apiVersion values and implement `DeepCopyObject()`:

```go
// Required by cache.SharedIndexInformer
func (d *ApplyDesire) GetObjectKind() schema.ObjectKind { return &d.TypeMeta }
func (d *ApplyDesire) DeepCopyObject() runtime.Object   { return d.DeepCopy() }

func (d *ApplyDesire) DeepCopy() *ApplyDesire {
    if d == nil { return nil }
    out := *d
    out.Spec = *d.Spec.DeepCopy()
    out.Status = *d.Status.DeepCopy()
    return &out
}
```

`DeepCopy()` is also required by the `desirestatuswriter` package's `DeepCopyable` constraint:
```go
type DeepCopyable[T any] interface {
    *T
    DeepCopy() *T
}
```

**List wrapper types** (required by informer's `ListWithContextFunc`):
```go
type ApplyDesireList struct {
    metav1.TypeMeta `json:",inline"`
    metav1.ListMeta `json:"metadata"`
    Items           []ApplyDesire `json:"items"`
}
// + GetObjectKind(), DeepCopyObject() methods
// Same pattern for DeleteDesireList, ReadDesireList
```

**`RawExtension` serialization — RESOLVED in Phase 2a:**
Firestore's Go SDK codec rejects `runtime.RawExtension` with `firestore: cannot convert type runtime.Object to value` (the interface check fails, not a byte-array issue as originally anticipated). `metav1.Time` inside `metav1.Condition.LastTransitionTime` works correctly — Firestore handles it as a native timestamp.

**Decision: Option 1** — `firestore:"-"` tag on `KubeContent` with manual serialization in the CRUD layer (`internal/database/rawext_codec.go`):
- On write: `json.Unmarshal(RawExtension.Raw)` → `map[string]any` → stored as root-level Firestore fields `spec_kubeContent` and `status_kubeContent`
- On read: Firestore map → `json.Marshal` → `RawExtension{Raw: bytes}`
- **Caveat:** JSON key ordering is not preserved through a round-trip (Firestore maps are unordered, `json.Marshal` sorts alphabetically). The data is semantically identical. Controllers compare via `bytes.Equal` on the marshaled output, so a byte mismatch on first write is expected and correct (it triggers the initial status write).

Each desire type implements `KubeContentAccessor` (get/set for spec and status `KubeContent`). Types without `KubeContent` (DeleteDesire) return nil from both getters; the codec skips serialization.

### 3. Database Core Interfaces

```go
// internal/database/types.go

// FirestoreMetadataAccessor — generic access to server-managed metadata.
// Implemented by FirestoreMetadata (embedded in all desire types).
type FirestoreMetadataAccessor interface {
    GetDocumentID() string;  SetDocumentID(string)
    GetUpdateTime() time.Time; SetUpdateTime(time.Time)
    GetCreateTime() time.Time; SetCreateTime(time.Time)
}

// SpecStatusAccessor — generic access to the two data fields stored in Firestore.
// Used by Replace (which calls firestore.Update on "spec" and "status" paths).
type SpecStatusAccessor interface {
    GetSpec() any
    GetStatus() any
}

// KubeContentAccessor — manual RawExtension serialization (see resolved spike above).
type KubeContentAccessor interface {
    GetSpecKubeContent() *runtime.RawExtension
    SetSpecKubeContent(*runtime.RawExtension)
    GetStatusKubeContent() *runtime.RawExtension
    SetStatusKubeContent(*runtime.RawExtension)
}

type ResourceCRUD[T any] interface {
    Get(ctx context.Context, documentID string) (*T, error)
    List(ctx context.Context) ([]*T, error)
    Create(ctx context.Context, obj *T) (*T, error)
    Replace(ctx context.Context, obj *T) (*T, error)  // uses LastUpdateTime precondition
    Delete(ctx context.Context, documentID string) error
}

type KubeApplierDBClient interface {
    ApplyDesires() ResourceCRUD[kubeapplier.ApplyDesire]
    DeleteDesires() ResourceCRUD[kubeapplier.DeleteDesire]
    ReadDesires() ResourceCRUD[kubeapplier.ReadDesire]
    Close() error
}
```

Informers and listers are constructed separately at app wiring time via
`informers.NewKubeApplierInformers(firestoreClient)`, not attached to `KubeApplierDBClient`.
The DB client is a CRUD handle; informers are a higher-level concern that wraps a Firestore
client directly (they need `collection.Snapshots()` and `collection.Documents().GetAll()`,
not the typed CRUD layer).

**Deliberate simplification from ARO HCP** (see "Deliberately eliminated" table above):
- **Drops `ResourceParent` and per-parent CRUD scoping** — Cosmos requires partition-keyed routing through `ForCluster()`/`ForNodePool()` methods. Firestore's per-database isolation eliminates this; controllers call `ResourceCRUD[T]` directly.
- **Drops narrow typed interfaces** (`KubeApplierApplyDesireCRUD`, etc.) — these existed solely to expose parent-scoped routing to individual controllers. With flat CRUD, controllers take `ResourceCRUD[T]` directly.
- **Drops `UntypedCRUD`** — Cosmos needed untyped document walking for orphan cleanup. Firestore replaces this with typed queries when the backend service needs them.
- **Drops `GenericDocument[T]` envelope** — Cosmos wraps documents in a partition-keyed envelope. Firestore stores desires directly; `FirestoreMetadata` fields use `firestore:"-"` and are populated from `DocumentSnapshot` server fields.

### 4. Firestore CRUD Implementation

```go
// internal/database/crud.go
//
// The type constraint requires FirestoreMetadataAccessor, SpecStatusAccessor,
// KubeContentAccessor, and DeepCopy — shared with the in-memory fake.
type firestoreDesireCRUD[T any, PT desire[T]] struct {
    client     *firestore.Client
    collection string // "applydesires", "deletedesires", "readdesires"
}
```

**`Replace` uses `Update`, not `Set`:** Firestore's `Set` method accepts `SetOption`
(e.g., `MergeAll`) but NOT `Precondition` (e.g., `LastUpdateTime`). `Update` accepts
`Precondition` and replaces only the named field paths. Since the only data fields are
`spec` and `status` (plus the manually serialized `spec_kubeContent`/`status_kubeContent`),
updating all four paths is equivalent to a full document overwrite.

```go
func (c *firestoreDesireCRUD[T, PT]) Replace(ctx context.Context, obj *T) (*T, error) {
    pt := PT(obj)
    docID := pt.GetDocumentID()
    updates := []firestore.Update{
        {Path: "spec", Value: pt.GetSpec()},
        {Path: "status", Value: pt.GetStatus()},
    }
    kubeUpdates, _ := kubeContentWriteUpdates(pt) // from rawext_codec.go
    updates = append(updates, kubeUpdates...)
    wr, err := c.col().Doc(docID).Update(ctx, updates, firestore.LastUpdateTime(pt.GetUpdateTime()))
    // ... error mapping: FailedPrecondition, NotFound
}
```

**`Create` uses `map[string]any`:** Because `KubeContent` is `firestore:"-"`, passing the
struct directly to `DocRef.Create()` would silently drop it. Instead, the CRUD layer builds
a `map[string]any{"spec": ..., "status": ..., "spec_kubeContent": ...}` and passes that.

**`snapshotToDesire`** calls `snap.DataTo(&obj)` for the struct fields, then
`kubeContentReadFromSnapshot(pt, snap.Data())` to reconstruct the `RawExtension` fields
from the root-level map entries.

### 5. Firestore Snapshot Listener -> Informer Bridge

The most architecturally significant adaptation. Wraps Firestore's real-time `Snapshots()` iterator into a `watch.Interface` that feeds `cache.SharedIndexInformer`:

```go
// internal/database/informers/listener_watcher.go
type firestoreWatcher struct {
    resultCh chan watch.Event  // buffered (100)
    done     chan struct{}
    cancel   context.CancelFunc
}

func newFirestoreWatcher(
    ctx context.Context,
    collection *firestore.CollectionRef,
    convertFn func(*firestore.DocumentSnapshot) (runtime.Object, error),
) *firestoreWatcher
```

The goroutine calls `collection.Snapshots(childCtx)` and loops on `snapIter.Next()`. Each `DocumentChange` is mapped: `DocumentAdded→Added`, `DocumentModified→Modified`, `DocumentRemoved→Deleted`. Events are sent with a context-aware select to prevent deadlock if the consumer stops. `Stop()` cancels the child context (idempotent).

Unlike ARO HCP's `expiringWatcher` (forces relist every 30s because Cosmos has no watch), the Firestore listener stays connected permanently. The informer's `ResyncPeriod` still triggers handler resyncs for cooldown-gated re-reconciliation.

**`listWatchWithoutWatchListSemantics` wrapper (required):** The informer's `ListWatch` is wrapped to opt out of client-go v0.35+'s WatchList streaming mode. Firestore's snapshot listener does not emit bookmark events; without this wrapper, the Reflector waits for a bookmark that never arrives and `WaitForCacheSync` blocks forever. This is the same wrapper needed by ReadDesireKubernetesController (Section 8).

**Error recovery semantics:** When the Firestore gRPC stream breaks (network blip, token refresh, Firestore maintenance), the watcher sends a `watch.Error` event with `StatusReasonExpired` and returns. The informer's `Reflector` handles this by:
1. Calling `ListWithContextFunc` to re-read the full collection (re-list)
2. Calling `WatchFuncWithContext` to establish a new snapshot listener
3. Applying internal backoff on repeated failures

This means `ListWithContextFunc` must always work as the re-list path after listener failure. Transient gRPC errors (`codes.Unavailable`, `codes.Internal`) are retried by the Reflector's backoff — the watcher does not need its own retry loop. Only `ctx.Err() != nil` (intentional shutdown) should exit silently.

**Exported helpers for the informer bridge:** The `database` package exports `SnapshotToApplyDesire`, `SnapshotToDeleteDesire`, `SnapshotToReadDesire` converter functions and `CollectionApplyDesires`, `CollectionDeleteDesires`, `CollectionReadDesires` collection name constants. These are consumed by the `informers` sub-package for its `ListWithContextFunc` and `WatchFuncWithContext`.

### 6. Informer Factory

```go
// internal/database/informers/informers.go
type KubeApplierInformers interface {
    ApplyDesires() (cache.SharedIndexInformer, listers.ApplyDesireLister)
    DeleteDesires() (cache.SharedIndexInformer, listers.DeleteDesireLister)
    ReadDesires() (cache.SharedIndexInformer, listers.ReadDesireLister)
    RunWithContext(ctx context.Context)
}

func NewKubeApplierInformers(client *firestore.Client) KubeApplierInformers
func NewKubeApplierInformersWithResyncPeriod(client *firestore.Client, resyncPeriod time.Duration) KubeApplierInformers
```

Each per-type informer is constructed via `newDesireInformer(collection, exampleObj, convertFn, listFn, resyncPeriod)` which builds a `cache.ListWatch` wrapped in `listWatchWithoutWatchListSemantics`. The `ListWithContextFunc` calls `collection.Documents(ctx).GetAll()` and converts via the type-specific `SnapshotTo*Desire` function. The `WatchFuncWithContext` returns a `newFirestoreWatcher`. No custom indexers are registered — the default `MetaNamespaceKeyFunc` store key (= DocumentID) is sufficient.

Listers (`List()`, `Get(documentID)`) are constructed from `informer.GetIndexer()`. No `ByCluster`/`ByNodePool` indexes — those are backend service concerns, not needed by the kube-applier controllers which process every desire in their database.

`RunWithContext` starts all three informers in goroutines and blocks on `<-ctx.Done()`.
```

### 7. Controller Change Detection

```go
// In each controller's handleUpdate:
func (c *ApplyDesireController) handleUpdate(oldObj, newObj any) {
    oldD, newD := oldObj.(*kubeapplier.ApplyDesire), newObj.(*kubeapplier.ApplyDesire)
    changed := !oldD.UpdateTime.Equal(newD.UpdateTime) // replaces GetEtag() comparison
    c.enqueueWithCooldown(newD, changed)
}
```

### 8. ReadDesireKubernetesController: Critical Patterns

**`listWatchWithoutWatchListSemantics` wrapper (required):** The per-instance kube informer uses `dynamic.Interface` Watch, which doesn't emit bookmark events required by client-go v0.35+'s WatchList streaming mode. Without this opt-out wrapper, the Reflector never reaches Synced and `cache.WaitForCacheSync()` blocks forever:

```go
type listWatchWithoutWatchListSemantics struct {
    *cache.ListWatch
}

func (listWatchWithoutWatchListSemantics) IsWatchListSemanticsUnSupported() bool { return true }
```

This is needed in both production and tests (fake clients also lack bookmark events).

**`HasSynced` defensive guard (required):** The controller must wait for `cache.WaitForCacheSync()` before processing any queue items. Without this, a freshly launched per-instance controller would incorrectly report "target absent" for an object that simply hasn't been listed yet.

**Byte-equal no-op detection:** Before writing status, compare `desire.Status.KubeContent.Raw` byte-for-byte against the observed kube object. ReadDesires re-sync every 60 seconds — without this check, every resync writes to Firestore. Even on byte-equal, still call UpdateStatus to flip the Successful condition from Unknown to True on the first cycle.

### 9. ReadDesireManager: Goroutine Lifecycle

The manager owns per-ReadDesire sub-controller goroutines. The lifecycle management is the most complex synchronization in the codebase:

```go
type runningInstance struct {
    target kubeapplier.ResourceReference
    cancel context.CancelFunc
    done   chan struct{}
}

// Protected by sync.Mutex
running map[keys.ReadDesireKey]*runningInstance
```

**Critical invariants:**
- `stopByKey()` must call `cancel()` **and** wait on `<-cur.done` before returning. Without the wait, informers and listers leak when replacing a listener on TargetItem change.
- `stopAll()` must be called in `defer` of `Run()` to clean up all instances on shutdown.
- Mutex protects the `running` map; lock ordering: lock → read/delete map → unlock → wait on `done` channel (never hold the lock while waiting).

```go
func (c *ReadDesireInformerManagingController) stopByKey(key keys.ReadDesireKey) {
    c.mu.Lock()
    cur, ok := c.running[key]
    if ok { delete(c.running, key) }
    c.mu.Unlock()
    if !ok { return }
    cur.cancel()
    <-cur.done // WAIT for goroutine to fully exit
}
```

### 10. Simplified Keys

**Deliberate simplification from ARO HCP:** The ARO `ApplyDesireKey` has 5 fields (SubscriptionID, ResourceGroupName, ClusterName, NodePoolName, Name) and a `CRUD(client)` method that routes to parent-scoped CRUD accessors. Both are eliminated: GCP keys have 3 fields, and controllers call `ResourceCRUD[T]` directly without routing.

```go
// pkg/controllers/keys/keys.go
type ApplyDesireKey struct {
    ClusterID    string
    NodePoolName string
    Name         string  // = Firestore DocumentID ({clusterID}--{desireName})
}

func ApplyDesireKeyFromDesire(d *kubeapplier.ApplyDesire) (ApplyDesireKey, error) {
    return ApplyDesireKey{
        ClusterID:    d.Spec.ClusterID,
        NodePoolName: d.Spec.NodePoolName,
        Name:         d.DocumentID,
    }, nil
}

func (k ApplyDesireKey) IsNodePoolScoped() bool { return k.NodePoolName != "" }
```

The Fetcher implementation becomes a trivial one-liner:
```go
type applyDesireFetcher struct {
    crud database.ResourceCRUD[kubeapplier.ApplyDesire]
}

func (f *applyDesireFetcher) Fetch(ctx context.Context, key keys.ApplyDesireKey) (*kubeapplier.ApplyDesire, error) {
    return f.crud.Get(ctx, key.Name)
}
```

### 11. Worker Threadiness & Run Loop Constants

```go
const (
    threadsApply       = 4  // concurrent ApplyDesire reconcilers
    threadsDelete      = 4  // concurrent DeleteDesire reconcilers
    threadsReadManager = 1  // bookkeeping only; per-instance controllers run independently

    leaderElectionLeaseDuration = 15 * time.Second
    leaderElectionRenewDeadline = 10 * time.Second
    leaderElectionRetryPeriod   = 2 * time.Second

    healthCheckTimeout  = 20 * time.Second // leader election staleness threshold
    httpShutdownTimeout = 31 * time.Second // intentionally > healthCheckTimeout + buffer
)
```

**Panic recovery:** Wire `kuberuntime.ReallyCrash = o.ExitOnPanic` at the top of `Run()`. Add `defer kuberuntime.HandleCrash()` to every goroutine (controller workers, HTTP servers, leader election callback).

### 12. CLI Flags

```go
// cmd/root.go
type KubeApplierRootCmdFlags struct {
    Kubeconfig                 string
    KubeNamespace              string
    ManagementCluster          string // Simple string (GKE cluster name)
    FirestoreProject           string // GCP project ID
    FirestoreDatabase          string // Named database ID (e.g., "mc-dev-westus3-1")
    MetricsServerListenAddress string
    HealthzServerListenAddress string
    LeaderElectionID           string
    LogVerbosity               int
    ExitOnPanic                bool
}
```

### 13. Client Construction

```go
// pkg/app/firestore_wiring.go
func NewKubeApplierDBClient(ctx context.Context, projectID, databaseID string) (database.KubeApplierDBClient, error) {
    // GKE Workload Identity auto-detects credentials via ADC.
    client, err := firestore.NewClientWithDatabase(ctx, projectID, databaseID)
    if err != nil {
        return nil, fmt.Errorf("failed to create Firestore client for database %s: %w", databaseID, err)
    }
    return database.NewFirestoreKubeApplierDBClient(client), nil
}
```

### 14. Multi-MC Backend Registry (for backend service, not this binary)

**Simplified from ARO HCP:** The ARO backend holds a `KubeApplierDBClients` registry that lazily constructs per-MC clients by walking a ManagementCluster lister to resolve Cosmos container names from MC status fields. With Firestore, database names are deterministic (`mc-{clusterName}`), so no lister walk is needed — construct the client directly from the MC name.

```go
// For the backend side (interface design only, out of scope for this binary):
type KubeApplierDBClients interface {
    For(managementClusterName string) KubeApplierDBClient
    ManagementClusterNames() []string
}
// Each client: firestore.NewClientWithDatabase(ctx, project, "mc-"+mcName)
// No lister-walk resolution needed — database name is deterministic from MC name.
```

---

## Infrastructure

### Terraform: Firestore Databases + IAM

```hcl
# terraform/firestore.tf
resource "google_firestore_database" "kube_applier" {
  for_each = toset(var.management_clusters)

  project     = var.project_id
  name        = "mc-${each.value}"
  location_id = var.region
  type        = "FIRESTORE_NATIVE"

  point_in_time_recovery_enablement = "POINT_IN_TIME_RECOVERY_ENABLED"
  delete_protection_state           = "DELETE_PROTECTION_ENABLED"
}

# terraform/iam.tf
resource "google_service_account" "kube_applier" {
  for_each     = toset(var.management_clusters)
  project      = var.project_id
  account_id   = "kube-applier-${each.value}"
  display_name = "Kube Applier for MC ${each.value}"
}

resource "google_project_iam_member" "kube_applier_firestore" {
  for_each = toset(var.management_clusters)
  project  = var.project_id
  role     = "roles/datastore.user"
  member   = "serviceAccount:${google_service_account.kube_applier[each.value].email}"

  condition {
    title      = "restrict-to-mc-database-${each.value}"
    expression = "resource.name == 'projects/${var.project_id}/databases/mc-${each.value}'"
  }
}

resource "google_service_account_iam_member" "kube_applier_workload_identity" {
  for_each           = toset(var.management_clusters)
  service_account_id = google_service_account.kube_applier[each.value].name
  role               = "roles/iam.workloadIdentityUser"
  member             = "serviceAccount:${var.project_id}.svc.id.goog[${var.kube_applier_namespace}/kube-applier]"
}
```

### Helm Chart Values

```yaml
# deploy/values.yaml
deployment:
  replicas: 2
  imageName: "gcr.io/project/kube-applier:latest"
  resources:
    requests:
      cpu: "100m"
      memory: "128Mi"
    limits:
      memory: "256Mi"
managementCluster: ""      # GKE cluster name
firestore:
  project: ""              # GCP project ID
  database: ""             # Named database ID
serviceAccount:
  name: "kube-applier"
  gcpServiceAccount: ""    # GSA email for Workload Identity
```

---

## Testing Strategy

### Unit Tests
- **Fake KubeApplierDBClient**: In-memory `map[string]*T` implementation with `UpdateTime`-based optimistic concurrency. Replaces ARO HCP's `MockKubeApplierDBClient`. Must track `UpdateTime` per document and return `codes.FailedPrecondition` on stale precondition.
- **Controller unit tests**: Same pattern as ARO HCP -- fake DB client + `k8s.io/client-go/dynamic/fake` + real informers backed by fakes. Test `SyncOnce` directly. Use `clocktesting.FakeClock` for cooldown gate tests.
- **Test matrices**: Port the exact test matrices from ARO HCP `docs/07-testing.md` for all four controllers.
- **`metav1.Condition` / `metav1.Time` round-trip**: Verify that conditions (especially `LastTransitionTime`) serialize and deserialize correctly through Firestore's codec.
- **`RawExtension` round-trip**: Verify `KubeContent` survives Firestore serialization without data corruption (see Section 2 caveat).

### Unit Test Matrices (from ARO HCP docs/07-testing.md)

**ApplyDesireController:**
| Case | Expected |
|------|----------|
| Valid manifest, apply succeeds | `Successful=True` |
| Invalid JSON in `kubeContent` | `Successful=False`, reason `PreCheckFailed` |
| Missing version/resource/name in targetItem | `Successful=False`, reason `PreCheckFailed` |
| Kube-apiserver returns 403 | `Successful=False`, reason `KubeAPIError`; Degraded NOT set (4xx) |
| Kube-apiserver returns 500 | `Successful=False`, reason `KubeAPIError`; `Degraded=True` (5xx) |
| Force=true resolves field manager conflict | `Successful=True`; verify diff applied |
| No-op resync (unchanged etag) | No Firestore write (verify via mock) |

**DeleteDesireController:**
| Case | Expected |
|------|----------|
| Target absent | `Successful=True` |
| Target 404 race (gone between Get and Delete) | `Successful=True` |
| Delete succeeds, object terminating (finalizers) | `Successful=False`, reason `WaitingForDeletion`, msg includes UID+DT |
| DeletionTimestamp already set | `Successful=False`, reason `WaitingForDeletion` |
| Delete returns 500 | `Successful=False`, reason `KubeAPIError` |

**ReadDesireKubernetesController:**
| Case | Expected |
|------|----------|
| Target exists at startup | `Status.KubeContent` populated, `Successful=True` |
| Target absent at startup (after HasSynced) | `Status.KubeContent=nil`, `Successful=True` |
| Target appears after startup | Status updated within resync period |
| Target disappears | Status cleared within 60s tick |
| Byte-equal resync (no-op) | No Firestore write |
| ListWatch error | `Successful=False`, reason `KubeAPIError` |

**ReadDesireInformerManagingController:**
| Case | Expected |
|------|----------|
| ReadDesire created | Per-instance controller launched |
| TargetItem changed | Old controller stopped (waited for done), new one launched |
| ReadDesire deleted | Per-instance stopped + removed from running map |
| Target unchanged (resync) | No-op, factory not called |
| Per-instance construction fails | `Successful=False`, reason `PreCheckFailed` |

### Integration Tests
- **Firestore emulator**: Set `FIRESTORE_EMULATOR_HOST` env var. The Go SDK auto-detects and connects to the emulator. Supports all operations including `Snapshots()`.
- **envtest**: Real kube-apiserver + etcd in-process (same as ARO HCP).
- **End-to-end**: Create desire in Firestore emulator -> controller picks it up -> SSA/delete/read on envtest apiserver -> status written back to emulator.
- **Optimistic concurrency conflict**: Two concurrent `Replace()` calls on the same document — first succeeds, second gets `codes.FailedPrecondition` — verify workqueue retries and second write eventually succeeds with fresh `UpdateTime`.
- **Listener reconnection**: Simulate Firestore listener disconnection (emulator supports this) — verify the Reflector re-lists and re-establishes the listener.
- **Per-database isolation**: Create desires in two separate `mc-*` databases — verify listeners don't cross-pollinate.

### Verification Checklist
- [ ] `go build ./...` passes
- [ ] Unit tests pass for all 4 controllers + database layer
- [ ] Integration test with Firestore emulator + envtest passes
- [ ] Binary starts, acquires leader lease, exposes `/healthz` and `/metrics`
- [ ] Desires created in Firestore are reconciled within one listener cycle
- [ ] Optimistic concurrency: concurrent status writes don't lose data
- [ ] Listener reconnects after transient gRPC error
- [ ] IAM isolation: pod with wrong GSA gets `codes.PermissionDenied`
- [ ] `RawExtension` / `metav1.Time` survive Firestore round-trip without corruption
- [ ] ReadDesireManager cleanup: all per-instance goroutines exit on shutdown

---

## Phased Rollout

### Phase 1: Foundation (Week 1-2) ✅ COMPLETE
**Single PR. Creates the module and all types with no runtime effect.**
- Create Go module (`go.mod`) with dependencies: k8s.io/apimachinery v0.36.1, k8s.io/utils, google.golang.org/grpc v1.81.1, Go 1.26.0
- Port API types: `FirestoreMetadata` (with accessor methods `GetDocumentID`, `GetUpdateTime`, `GetCreateTime`), desire types (adapted), `ResourceReference`, conditions
- Implement `runtime.Object` compliance: `GetObjectKind()`, `DeepCopyObject()`, `GetObjectMeta()` (ObjectMetaAccessor) on all desire types
- Implement hand-written `DeepCopy() *T` and `DeepCopyInto()` methods on all desire types (required by `desirestatuswriter`'s `DeepCopyable` constraint)
- Define list wrapper types: `ApplyDesireList`, `DeleteDesireList`, `ReadDesireList` (required by informer `ListWithContextFunc`)
- Port utility packages: `controllerutils/cooldown.go`
- Port conditions package: `conditions.go`
- Port status writer: `desirestatuswriter/` (depends on `database.IsNotFoundError` — coordinated with `errors.go`)
- Implement keys package with simplified 3-field GCP key structure
- Implement `database/errors.go` with gRPC-based `IsNotFoundError` (`codes.NotFound`), `IsPreconditionFailedError` (`codes.FailedPrecondition`), plus `NewNotFoundError()` / `NewPreconditionFailedError()` constructors for test fakes
- Unit tests for type marshaling, DeepCopy isolation, key construction, error classification, conditions, cooldown
- **Deferred to Phase 2a:** `RawExtension` / `metav1.Time` Firestore round-trip spike (requires Firestore client or emulator). Phase 1 uses `firestore:"kubeContent,omitempty"` as placeholder tag.

### Phase 2a: Database CRUD + Fakes (Week 2) ✅ COMPLETE
**Single PR. Implements Firestore CRUD and in-memory fakes. No informers yet — this is the data access layer only.**
- **Spike resolved:** `RawExtension` fails with `firestore: cannot convert type runtime.Object to value`. Fixed with `firestore:"-"` + manual serialization in `rawext_codec.go`. `metav1.Time` works natively. JSON key ordering is not preserved (Firestore maps are unordered) but data is semantically identical.
- `database/types.go` -- `ResourceCRUD[T]`, `FirestoreMetadataAccessor`, `SpecStatusAccessor`, `KubeContentAccessor`, `KubeApplierDBClient` interfaces
- `database/client.go` -- `firestoreKubeApplierDBClient` impl (scoped to three collections)
- `database/crud.go` -- `firestoreDesireCRUD[T, PT]` with `Get`, `List`, `Create`, `Replace` (using `Update` + `LastUpdateTime`), `Delete`
- `database/rawext_codec.go` -- manual `RawExtension` serialization (JSON ↔ `map[string]any`)
- `database/errors.go` -- (Phase 1, wired into CRUD)
- `database/listertesting/fake_crud.go` -- generic `FakeCRUD[T, PT]` with deterministic `UpdateTime` (monotonic counter)
- `database/listertesting/fake_client.go` -- `FakeKubeApplierDBClient` wrapping three FakeCRUDs
- 18 unit tests against fakes + 11 integration tests against Firestore emulator (all pass)
- Desire types gained: `GetSpec()/GetStatus()`, `GetSpecKubeContent()/SetSpecKubeContent()` etc., `SetDocumentID()/SetUpdateTime()/SetCreateTime()`
- **Key deviation from plan:** `Replace` uses `firestore.Update` (not `Set`) because `Set` does not accept `LastUpdateTime` precondition. `Create` passes `map[string]any` (not struct) to include manually serialized KubeContent.

**Exit criteria met:** `database.ResourceCRUD[T]` works end-to-end against both fakes and the emulator. Controllers can be built against fakes without waiting for Phase 2b.

### Phase 2b: Informer Bridge + Listers (Week 2-3) ✅ COMPLETE
**Single PR. Bridges Firestore listeners into the k8s informer/lister machinery. This is the most novel component.**
- `database/informers/listener_watcher.go` -- `firestoreWatcher` wraps Firestore `collection.Snapshots()` → `watch.Interface`. Maps `DocumentAdded/Modified/Removed` to `watch.Added/Modified/Deleted`. On stream error: sends `watch.Error` with `StatusReasonExpired` and exits; Reflector handles re-list + re-watch with backoff. Context-aware event send prevents deadlock.
- `database/informers/list_watch.go` -- `listWatchWithoutWatchListSemantics` wrapper (opts out of client-go WatchList mode; Firestore has no bookmark events)
- `database/informers/informers.go` -- `KubeApplierInformers` interface + factory. `ListWithContextFunc` calls `collection.Documents(ctx).GetAll()` and converts via exported `SnapshotTo*Desire` functions. `WatchFuncWithContext` returns `newFirestoreWatcher`. `RunWithContext` starts 3 informer goroutines.
- `database/listers/` -- typed listers with `List()` and `Get(documentID)` backed by `cache.Indexer`. No custom indexes (`ByCluster`/`ByNodePool` are backend service concerns, not needed by kube-applier controllers).
- `database/crud.go` gained exported `SnapshotToApplyDesire`, `SnapshotToDeleteDesire`, `SnapshotToReadDesire` converter functions
- `database/client.go` gained exported collection name constants (`CollectionApplyDesires`, etc.)
- Added `k8s.io/client-go v0.36.1` dependency (provides `tools/cache`)
- 6 unit tests (lister List/Get against populated cache, Get returns NotFoundError, WatchList wrapper, all 3 lister types)
- 4 integration tests against Firestore emulator (initial sync of pre-existing docs, live create/modify/delete event delivery, per-database isolation, all 3 informer types with KubeContent round-trip)
- **Key deviation from plan:** Informers are constructed separately via `NewKubeApplierInformers(firestoreClient)`, not attached to `KubeApplierDBClient.Listers()`. The DB client is a CRUD handle; informers wrap the Firestore client directly for `Snapshots()` and `Documents().GetAll()`.

**Exit criteria met:** `SharedIndexInformer` backed by Firestore listener works end-to-end against the emulator. Informer syncs, delivers Add/Update/Delete events, and recovers from listener disconnection via Reflector re-list.

### Phase 3: Controllers (Week 3-4)
**One PR per controller pair.**
- 3a: `ApplyDesireController` + `DeleteDesireController` (adapted: `UpdateTime` replaces etag, `FieldManager = "gcp-hcp-kube-applier"`)
- 3b: `ReadDesireInformerManagingController` (with goroutine lifecycle: `runningInstance` struct, `stopByKey()` with `<-done` wait, `stopAll()` in `defer Run()`)
- 3b: `ReadDesireKubernetesController` (with `listWatchWithoutWatchListSemantics` wrapper, `HasSynced` guard, byte-equal no-op detection)
- Unit tests for all controllers per test matrices above
- Integration test with Firestore emulator + envtest

### Phase 4: Binary Wiring (Week 4)
**Single PR. Makes the binary runnable.**
- `cmd/root.go` with GCP flags + validation (catch `--flag=` edge cases)
- `pkg/app/options.go`, `firestore_wiring.go`, `kube_applier.go` run loop
- Wire `kuberuntime.ReallyCrash = o.ExitOnPanic` + `defer kuberuntime.HandleCrash()` on all goroutines
- Wire shutdown timing: 31s HTTP shutdown, 20s leader health check adaptor
- Wire worker threadiness: 4/4/1 for apply/delete/readManager
- Error aggregation: buffered `errCh`, `errors.Join()`, filter `http.ErrServerClosed`
- Port `kube_wiring.go`, `leader_election_wiring.go`
- Dockerfile + Makefile
- End-to-end smoke test with emulator

### Phase 5: Deployment (Week 5)
**Single PR. Infrastructure and deployment.**
- Terraform for Firestore databases + IAM + Workload Identity
- Helm chart (adapted from ARO HCP)
- CI pipeline

### Phase 6: Validation (Week 6)
- Deploy to dev management cluster
- Functional testing with real Firestore
- Performance validation (10k desires)
- IAM isolation verification

---

## Critical Source Files (ARO HCP Reference)

| ARO HCP File | What to learn from it |
|---|---|
| `kube-applier/pkg/controllers/apply_desire/controller.go` | Complete controller pattern: cooldown, etag-based change detection, SSA, status writer integration |
| `kube-applier/pkg/controllers/desirestatuswriter/desirestatuswriter.go` | Generic fetch-mutate-replace with optimistic concurrency |
| `kube-applier/pkg/app/kube_applier.go` | Run loop: leader election, informer startup, controller wiring |
| `kube-applier/cmd/root.go` | Flag definition, validation, `ToKubeApplierOptions` pattern |
| `internal/database/kube_applier_client.go` | `KubeApplierDBClient` interface -- template for Firestore version |
| `kube-applier/pkg/controllers/conditions/conditions.go` | Condition setters -- port verbatim |
| `kube-applier/pkg/controllers/keys/keys.go` | Key type -- simplify for GCP |
