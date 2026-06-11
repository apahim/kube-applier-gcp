
`kube-applier` is a per-management-cluster controller binary that runs on GKE and brokers
between Google Cloud Firestore and the local Kubernetes apiserver. It reads Desire documents
from Firestore and reconciles them against the cluster.

At a high level:
1. `ApplyDesire` indicates a kube manifest in `.spec.kubeContent` to issue a server-side-apply for.
   Success/failure is written to `.status.conditions["Successful"]`.
2. `DeleteDesire` indicates a kube item in `.spec.targetItem` to issue a delete for.
   Success/failure is written to `.status.conditions["Successful"]`.
3. `ReadDesire` indicates a kube item in `.spec.targetItem` to issue a list/watch+informer for.
   The observed content is written to `.status.kubeContent`.
   Success/failure is written to `.status.conditions["Successful"]`.

## Scale
The scale of the kube-applier is tiny: it covers a single management cluster.
A single management cluster will have a low hundreds of HostedClusters and if we have about 100 *Desires, we end up
with about 10k *Desires.
Ten thousand is such a small number that with simple poll and iterate at 50 qps, we can scan every three minutes.
We'll probably actually use a larger burst and smaller QPS, but it's an easy scale to manage.
The scale of a region is larger, but is handled by Firestore so it will scale far beyond our needs.

## API structure
The API types for this live in `internal/api/kubeapplier`.

Every `*Desire` API interacts with a single kubernetes resource instance.
We do not support lists, label selection, or list-all.
This is for simplicity in reasoning about the status.

### ManagementCluster
Every `*Desire` API has a `.spec.managementCluster` field.
This is the name of the GKE management cluster that the `kube-applier` is running in.
It matches the value the kube-applier binary was started with via `--management-cluster`.
Each management cluster has its own Firestore named database (`mc-{clusterName}`),
so the management cluster name determines which database the binary connects to.

### Conditions
Each `*Desire` API has a list of conditions.
One of those conditions is the "Successful" condition.
Successful is true if the operation succeeded.
1. For ApplyDesire, this means a successful server-side-apply.
2. For DeleteDesire, this means the item is no longer present in the cluster.
   This is NOT the same as the delete call succeeded, remember that kubernetes has finalizers.
3. For ReadDesire, this means the list/watch succeeded and the informer synced.

When the kube-apiserver call fails,
1. `.status.conditions["Successful"].status` is false
2. `.status.conditions["Successful"].reason` is "KubeAPIError"
3. `.status.conditions["Successful"].message` is the error message from the kube-apiserver call.

When the kube-apiserver call cannot be executed,
1. `.status.conditions["Successful"].status` is false
2. `.status.conditions["Successful"].reason` is "PreCheckFailed"
3. `.status.conditions["Successful"].message` is whatever prevented us from calling the kube-apiserver.

## Database structure
Every management cluster has its own Firestore **named database**: `mc-{clusterName}`.
Each kube-applier pod authenticates via GKE Workload Identity Federation
(pod KSA → IAM GSA) with an IAM condition restricting access to exactly its own database.

Each database contains three collections:
```
database: mc-{managementClusterName}
  applydesires/{desireName}
  deletedesires/{desireName}
  readdesires/{desireName}
```

Document IDs are the desire name. Since a single MC can manage multiple clusters,
desire names must be globally unique within the MC database. The backend is responsible
for generating unique names (e.g., by including cluster/nodepool in the desire name).

The per-database layout means an escape from one management cluster's pod cannot read
or write another management cluster's Desires — there is no shared database to leak through.

Firestore snapshot listeners provide real-time change notification: the kube-applier
opens a persistent gRPC stream per collection and receives document changes as they happen,
rather than polling.

### Authentication and isolation
- GKE Workload Identity Federation: pod KSA → IAM GSA (no service account keys)
- Per-database IAM condition: `resource.name == "projects/{project}/databases/mc-{cluster}"`
- Role: `roles/datastore.user` (read + write)

### Golang type details for Database
The golang types live in `internal/database`.

`KubeApplierDBClient` is the per-database handle. It carries an open Firestore client
and exposes:
- `ApplyDesires() ResourceCRUD[ApplyDesire]`
- `DeleteDesires() ResourceCRUD[DeleteDesire]`
- `ReadDesires() ResourceCRUD[ReadDesire]`
- `Listers()` — per-database cross-type listers for feeding informers.

`ResourceCRUD[T]` provides flat CRUD operations: `Get`, `List`, `Create`, `Replace`, `Delete`.
`Replace` uses Firestore's `LastUpdateTime` precondition for optimistic concurrency —
if the document has changed since the last read, the write fails with `codes.FailedPrecondition`
and the controller retries with a fresh read.

Each desire type carries a `FirestoreMetadata` struct with:
- `DocumentID` — the Firestore document path
- `UpdateTime` — server-managed timestamp, used as the optimistic concurrency token
- `CreateTime` — server-managed creation timestamp

These fields use `firestore:"-"` tags and are populated from the `DocumentSnapshot`
server fields, not stored as document data.

The kube-applier binary opens exactly one database — its own — via
`NewKubeApplierDBClient(ctx, project, "mc-"+mcName)`.
The backend service constructs clients for each MC database deterministically
from the MC name; no registry or lister walk is needed.

The `internal/database/informers`, `internal/database/listers`, and
`internal/database/listertesting` packages provide the informers and listers for the
`*Desire` APIs.

## Controller structure
The `kube-applier` binary is controller-based with several controllers.
Instead of using a `Controller` type to communicate `Degraded` status, that is communicated
on the `*Desire` `.status.conditions["Degraded"]` field.

Change detection uses `UpdateTime` comparison: a controller's `handleUpdate` only
enqueues work when `!oldD.UpdateTime.Equal(newD.UpdateTime)`. The field manager for
server-side-apply is `gcp-hcp-kube-applier`.

### ReadDesireKubernetesController
An instance of this controller is created and started for each `ReadDesire` instance.
Each instance holds:
1. the `.spec.targetItem`
2. the `ReadDesireLister`
3. a single-item kubernetes informer
4. a single-item kubernetes lister
5. a `KubeApplierDBClient`
6. the document ID of the `ReadDesire` instance

In addition to running when the informer triggers, the controller unconditionally runs every one minute.
We do this so that if the item doesn't exist, we can properly report that.

When the sync loop runs, we read the item from the kubernetes lister and from the `ReadDesireLister` and compare the
`.status.kubeContent` against the kubernetes lister result.
If they are different, we update the `.status.kubeContent` and write it back to the database.

### ReadDesireInformerManagingController
This controller uses the `ReadDesire` informer to feed a sync function for `ReadDesire` instances.
Each time a particular `ReadDesire.spec.targetItem` changes — that is, the
GVR, namespace, or name identifying the kube object to watch (not changes to
the watched object's own content) — the old `ReadDesireKubernetesController`
instance is stopped, discarded, and a new one created.

The manager does not publish a per-launch status condition. The
`ReadDesireKubernetesController` itself owns `Successful` and the
`.status.kubeContent` field, which together carry whether the watch is
working. A separate "watch was last (re)launched at" timestamp turned out
to be uninterpretable — consumers cannot distinguish a target-driven
relaunch from a process restart — so it is not surfaced.

When a `ReadDesire` is deleted, the `ReadDesireKubernetesController` instance is stopped and discarded.

### DeleteDesireController
This controller uses the `DeleteDesire` informer to feed a sync function for `DeleteDesire` instances.
When the sync loop runs, it will:
1. Issue a get for the `.spec.targetItem`
   1. If it doesn't exist, write success and return
   2. If it does exist and has a deletion timestamp, indicate:
      1. `.status.conditions["Successful"].status` is false
      2. `.status.conditions["Successful"].reason` is "WaitingForDeletion"
      3. `.status.conditions["Successful"].message` contains a message that includes the deletion timestamp and UID
      4. and return
   3. If it does exist and has no deletion timestamp:
      1. Issue a delete for the `.spec.targetItem`.
         1. If unsuccessful, use the standard rule for `.status.conditions["Successful"]` and return
         2. If successful, issue a get for the deletion timestamp, indicate:
            1. `.status.conditions["Successful"].status` is false
            2. `.status.conditions["Successful"].reason` is "WaitingForDeletion"
            3. `.status.conditions["Successful"].message` contains a message that includes the deletion timestamp and UID
            4. and return

This controller resyncs every 60 seconds.

### ApplyDesireController
This controller uses the `ApplyDesire` informer to feed a sync function for `ApplyDesire` instances.
When the sync loop runs, it will:
1. Issue a server-side apply with force the `.spec.kubeContent`
2. Use the standard rules for `.status.conditions["Successful"]`

#### Adopting existing resources
SSA's `force=true` claims field ownership over fields the kube-applier writes
even if a different field manager owned them previously, but it does **not**
delete fields the prior owner wrote that are no longer in our object — those
remain owned by the prior manager. Adopting resources that pre-date the
kube-applier (e.g. created by hand or by another controller) therefore needs a one-time
sweep to clear stale managedFields entries, or careful authoring of the
ApplyDesire's `.spec.kubeContent` to cover every field of interest. We solve
this case-by-case rather than baking adoption logic into the kube-applier.

## Testing
Unit tests use the `internal/database/listertesting` package to create fake Firestore-compatible
database clients with `UpdateTime`-based optimistic concurrency tracking.

Integration tests use [envtest](https://book.kubebuilder.io/reference/envtest.html)
(via `sigs.k8s.io/controller-runtime`) to bring up a real `kube-apiserver` +
`etcd` in-process, paired with the Firestore emulator (`FIRESTORE_EMULATOR_HOST`).
envtest gives us the actual SSA conflict and admission semantics that a fake client
cannot reproduce, without the Docker dependency a `kind`-based suite would need.
