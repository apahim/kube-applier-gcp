package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"cloud.google.com/go/firestore"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	sigsyaml "sigs.k8s.io/yaml"

	kubeapplier "github.com/openshift/kube-applier-gcp/internal/api/kubeapplier"
	"github.com/openshift/kube-applier-gcp/internal/database"
	"github.com/openshift/kube-applier-gcp/internal/desireid"
)

var (
	flagProject        string
	flagSpecsDatabase  string
	flagStatusDatabase string
)

func main() {
	root := &cobra.Command{
		Use:   "desire-tool",
		Short: "CLI for creating and inspecting kube-applier desires in Firestore",
	}

	root.PersistentFlags().StringVar(&flagProject, "project", "test-project", "GCP project ID")
	root.PersistentFlags().StringVar(&flagSpecsDatabase, "specs-database", "mc-dev-local-specs", "Firestore named database ID for spec documents")
	root.PersistentFlags().StringVar(&flagStatusDatabase, "status-database", "mc-dev-local-status", "Firestore named database ID for status documents")

	root.AddCommand(
		newCreateApplyCmd(),
		newCreateDeleteCmd(),
		newCreateReadCmd(),
		newUpdateApplyCmd(),
		newUpdateReadCmd(),
		newGetApplyCmd(),
		newGetDeleteCmd(),
		newGetReadCmd(),
		newListApplyCmd(),
		newListDeleteCmd(),
		newListReadCmd(),
		newDeleteApplyCmd(),
		newDeleteDeleteCmd(),
		newDeleteReadCmd(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// twoDBClients creates two KubeApplierDBClient instances — one backed by the
// specs database and one by the status database. Each uses the same Firestore
// client for both the spec-reader and status-CRUD slots, so the desire-tool
// can perform full CRUD on either database via .XxxDesireStatus().
func twoDBClients(ctx context.Context) (specsDB, statusDB database.KubeApplierDBClient, cleanup func()) {
	specsClient, err := firestore.NewClientWithDatabase(ctx, flagProject, flagSpecsDatabase)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to connect to Firestore specs database: %v\n", err)
		os.Exit(1)
	}
	statusClient, err := firestore.NewClientWithDatabase(ctx, flagProject, flagStatusDatabase)
	if err != nil {
		specsClient.Close()
		fmt.Fprintf(os.Stderr, "Error: failed to connect to Firestore status database: %v\n", err)
		os.Exit(1)
	}
	specsDB = database.NewFirestoreKubeApplierDBClient(specsClient, specsClient)
	statusDB = database.NewFirestoreKubeApplierDBClient(statusClient, statusClient)
	return specsDB, statusDB, func() { specsDB.Close(); statusDB.Close() }
}

// --- create ---

func newCreateApplyCmd() *cobra.Command {
	var taskKey, clusterID, nodePool, filePath string

	cmd := &cobra.Command{
		Use:   "create-apply",
		Short: "Create an ApplyDesire from a YAML/JSON manifest file",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			specsDB, _, cleanup := twoDBClients(ctx)
			defer cleanup()

			raw, err := os.ReadFile(filePath)
			if err != nil {
				return fmt.Errorf("reading manifest file: %w", err)
			}

			raw, err = sigsyaml.YAMLToJSON(raw)
			if err != nil {
				return fmt.Errorf("converting manifest to JSON: %w", err)
			}

			ref, err := resourceRefFromManifest(raw)
			if err != nil {
				return fmt.Errorf("parsing manifest: %w", err)
			}

			docID := desireid.NewDocumentID(taskKey, ref.Group, ref.Version, ref.Resource, ref.Namespace, ref.Name)

			desire := &kubeapplier.ApplyDesire{
				Spec: kubeapplier.ApplyDesireSpec{
					ManagementCluster: "dev-local",
					ClusterID:         clusterID,
					NodePoolName:      nodePool,
					TargetItem:        ref,
					KubeContent:       &runtime.RawExtension{Raw: raw},
				},
			}
			desire.SetDocumentID(docID)

			created, err := specsDB.ApplyDesireStatus().Create(ctx, desire)
			if err != nil {
				return fmt.Errorf("creating ApplyDesire: %w", err)
			}

			fmt.Printf("Created ApplyDesire: %s\n", created.GetDocumentID())
			fmt.Printf("  Target: %s/%s %s/%s\n", ref.Version, ref.Resource, ref.Namespace, ref.Name)
			return nil
		},
	}

	cmd.Flags().StringVar(&taskKey, "task-key", "", "Task key for UUID v5 document ID generation (required)")
	cmd.Flags().StringVar(&clusterID, "cluster-id", "", "Cluster ID (required)")
	cmd.Flags().StringVar(&nodePool, "node-pool", "", "Node pool name (optional)")
	cmd.Flags().StringVar(&filePath, "file", "", "Path to YAML/JSON manifest (required)")
	cmd.MarkFlagRequired("task-key")
	cmd.MarkFlagRequired("cluster-id")
	cmd.MarkFlagRequired("file")

	return cmd
}

func newCreateDeleteCmd() *cobra.Command {
	var taskKey, clusterID, nodePool string
	var group, version, resource, namespace, resourceName string

	cmd := &cobra.Command{
		Use:   "create-delete",
		Short: "Create a DeleteDesire targeting a specific resource",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			specsDB, _, cleanup := twoDBClients(ctx)
			defer cleanup()

			docID := desireid.NewDocumentID(taskKey, group, version, resource, namespace, resourceName)

			desire := &kubeapplier.DeleteDesire{
				Spec: kubeapplier.DeleteDesireSpec{
					ManagementCluster: "dev-local",
					ClusterID:         clusterID,
					NodePoolName:      nodePool,
					TargetItem: kubeapplier.ResourceReference{
						Group:     group,
						Version:   version,
						Resource:  resource,
						Namespace: namespace,
						Name:      resourceName,
					},
				},
			}
			desire.SetDocumentID(docID)

			created, err := specsDB.DeleteDesireStatus().Create(ctx, desire)
			if err != nil {
				return fmt.Errorf("creating DeleteDesire: %w", err)
			}

			fmt.Printf("Created DeleteDesire: %s\n", created.GetDocumentID())
			fmt.Printf("  Target: %s/%s %s/%s\n", version, resource, namespace, resourceName)
			return nil
		},
	}

	cmd.Flags().StringVar(&taskKey, "task-key", "", "Task key for UUID v5 document ID generation (required)")
	cmd.Flags().StringVar(&clusterID, "cluster-id", "", "Cluster ID (required)")
	cmd.Flags().StringVar(&nodePool, "node-pool", "", "Node pool name (optional)")
	cmd.Flags().StringVar(&group, "group", "", "API group (empty for core)")
	cmd.Flags().StringVar(&version, "version", "", "API version (required)")
	cmd.Flags().StringVar(&resource, "resource", "", "Resource type (required)")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Namespace (empty for cluster-scoped)")
	cmd.Flags().StringVar(&resourceName, "resource-name", "", "Resource name (required)")
	cmd.MarkFlagRequired("task-key")
	cmd.MarkFlagRequired("cluster-id")
	cmd.MarkFlagRequired("version")
	cmd.MarkFlagRequired("resource")
	cmd.MarkFlagRequired("resource-name")

	return cmd
}

func newCreateReadCmd() *cobra.Command {
	var taskKey, clusterID, nodePool string
	var group, version, resource, namespace, resourceName string

	cmd := &cobra.Command{
		Use:   "create-read",
		Short: "Create a ReadDesire targeting a specific resource",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			specsDB, _, cleanup := twoDBClients(ctx)
			defer cleanup()

			docID := desireid.NewDocumentID(taskKey, group, version, resource, namespace, resourceName)

			desire := &kubeapplier.ReadDesire{
				Spec: kubeapplier.ReadDesireSpec{
					ManagementCluster: "dev-local",
					ClusterID:         clusterID,
					NodePoolName:      nodePool,
					TargetItem: kubeapplier.ResourceReference{
						Group:     group,
						Version:   version,
						Resource:  resource,
						Namespace: namespace,
						Name:      resourceName,
					},
				},
			}
			desire.SetDocumentID(docID)

			created, err := specsDB.ReadDesireStatus().Create(ctx, desire)
			if err != nil {
				return fmt.Errorf("creating ReadDesire: %w", err)
			}

			fmt.Printf("Created ReadDesire: %s\n", created.GetDocumentID())
			fmt.Printf("  Target: %s/%s %s/%s\n", version, resource, namespace, resourceName)
			return nil
		},
	}

	cmd.Flags().StringVar(&taskKey, "task-key", "", "Task key for UUID v5 document ID generation (required)")
	cmd.Flags().StringVar(&clusterID, "cluster-id", "", "Cluster ID (required)")
	cmd.Flags().StringVar(&nodePool, "node-pool", "", "Node pool name (optional)")
	cmd.Flags().StringVar(&group, "group", "", "API group (empty for core)")
	cmd.Flags().StringVar(&version, "version", "", "API version (required)")
	cmd.Flags().StringVar(&resource, "resource", "", "Resource type (required)")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Namespace (empty for cluster-scoped)")
	cmd.Flags().StringVar(&resourceName, "resource-name", "", "Resource name (required)")
	cmd.MarkFlagRequired("task-key")
	cmd.MarkFlagRequired("cluster-id")
	cmd.MarkFlagRequired("version")
	cmd.MarkFlagRequired("resource")
	cmd.MarkFlagRequired("resource-name")

	return cmd
}

// --- update ---

func newUpdateApplyCmd() *cobra.Command {
	var docID, filePath string

	cmd := &cobra.Command{
		Use:   "update-apply",
		Short: "Update an existing ApplyDesire's manifest (read-modify-write with optimistic concurrency)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			specsDB, _, cleanup := twoDBClients(ctx)
			defer cleanup()

			existing, err := specsDB.ApplyDesireStatus().Get(ctx, docID)
			if err != nil {
				return fmt.Errorf("getting ApplyDesire %s: %w", docID, err)
			}

			raw, err := os.ReadFile(filePath)
			if err != nil {
				return fmt.Errorf("reading manifest file: %w", err)
			}

			raw, err = sigsyaml.YAMLToJSON(raw)
			if err != nil {
				return fmt.Errorf("converting manifest to JSON: %w", err)
			}

			ref, err := resourceRefFromManifest(raw)
			if err != nil {
				return fmt.Errorf("parsing manifest: %w", err)
			}

			existing.Spec.KubeContent = &runtime.RawExtension{Raw: raw}
			existing.Spec.TargetItem = ref

			updated, err := specsDB.ApplyDesireStatus().Replace(ctx, existing)
			if err != nil {
				return fmt.Errorf("updating ApplyDesire: %w", err)
			}

			fmt.Printf("Updated ApplyDesire: %s\n", updated.GetDocumentID())
			fmt.Printf("  Target: %s/%s %s/%s\n", ref.Version, ref.Resource, ref.Namespace, ref.Name)
			return nil
		},
	}

	cmd.Flags().StringVar(&docID, "doc-id", "", "Document ID (required)")
	cmd.Flags().StringVar(&filePath, "file", "", "Path to updated YAML/JSON manifest (required)")
	cmd.MarkFlagRequired("doc-id")
	cmd.MarkFlagRequired("file")

	return cmd
}

func newUpdateReadCmd() *cobra.Command {
	var docID string
	var group, version, resource, namespace, resourceName string

	cmd := &cobra.Command{
		Use:   "update-read",
		Short: "Update an existing ReadDesire's target (read-modify-write with optimistic concurrency)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			specsDB, _, cleanup := twoDBClients(ctx)
			defer cleanup()

			existing, err := specsDB.ReadDesireStatus().Get(ctx, docID)
			if err != nil {
				return fmt.Errorf("getting ReadDesire %s: %w", docID, err)
			}

			existing.Spec.TargetItem = kubeapplier.ResourceReference{
				Group:     group,
				Version:   version,
				Resource:  resource,
				Namespace: namespace,
				Name:      resourceName,
			}

			updated, err := specsDB.ReadDesireStatus().Replace(ctx, existing)
			if err != nil {
				return fmt.Errorf("updating ReadDesire: %w", err)
			}

			fmt.Printf("Updated ReadDesire: %s\n", updated.GetDocumentID())
			fmt.Printf("  Target: %s/%s %s/%s\n", version, resource, namespace, resourceName)
			return nil
		},
	}

	cmd.Flags().StringVar(&docID, "doc-id", "", "Document ID (required)")
	cmd.Flags().StringVar(&group, "group", "", "API group (empty for core)")
	cmd.Flags().StringVar(&version, "version", "", "API version (required)")
	cmd.Flags().StringVar(&resource, "resource", "", "Resource type (required)")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Namespace (empty for cluster-scoped)")
	cmd.Flags().StringVar(&resourceName, "resource-name", "", "Resource name (required)")
	cmd.MarkFlagRequired("doc-id")
	cmd.MarkFlagRequired("version")
	cmd.MarkFlagRequired("resource")
	cmd.MarkFlagRequired("resource-name")

	return cmd
}

// --- get ---

func newGetApplyCmd() *cobra.Command {
	var docID string

	cmd := &cobra.Command{
		Use:   "get-apply",
		Short: "Get a single ApplyDesire (spec from specs-db, status from status-db)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			specsDB, statusDB, cleanup := twoDBClients(ctx)
			defer cleanup()
			return getApply(ctx, specsDB, statusDB, docID)
		},
	}

	cmd.Flags().StringVar(&docID, "doc-id", "", "Document ID (required)")
	cmd.MarkFlagRequired("doc-id")

	return cmd
}

func newGetDeleteCmd() *cobra.Command {
	var docID string

	cmd := &cobra.Command{
		Use:   "get-delete",
		Short: "Get a single DeleteDesire (spec from specs-db, status from status-db)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			specsDB, statusDB, cleanup := twoDBClients(ctx)
			defer cleanup()
			return getDelete(ctx, specsDB, statusDB, docID)
		},
	}

	cmd.Flags().StringVar(&docID, "doc-id", "", "Document ID (required)")
	cmd.MarkFlagRequired("doc-id")

	return cmd
}

func newGetReadCmd() *cobra.Command {
	var docID string

	cmd := &cobra.Command{
		Use:   "get-read",
		Short: "Get a single ReadDesire (spec from specs-db, status from status-db)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			specsDB, statusDB, cleanup := twoDBClients(ctx)
			defer cleanup()
			return getRead(ctx, specsDB, statusDB, docID)
		},
	}

	cmd.Flags().StringVar(&docID, "doc-id", "", "Document ID (required)")
	cmd.MarkFlagRequired("doc-id")

	return cmd
}

func getApply(ctx context.Context, specsDB, statusDB database.KubeApplierDBClient, docID string) error {
	spec, err := specsDB.ApplyDesireSpecs().Get(ctx, docID)
	if err != nil {
		return fmt.Errorf("getting ApplyDesire spec: %w", err)
	}
	printDesireHeader("ApplyDesire", spec.GetDocumentID(), spec.GetUpdateTime().String(), spec.GetCreateTime().String())
	fmt.Printf("  Cluster ID:   %s\n", spec.Spec.ClusterID)
	if spec.Spec.NodePoolName != "" {
		fmt.Printf("  Node Pool:    %s\n", spec.Spec.NodePoolName)
	}
	printTargetItem(spec.Spec.TargetItem)
	if spec.Spec.KubeContent != nil {
		fmt.Printf("  KubeContent:\n")
		printIndentedJSON(spec.Spec.KubeContent.Raw, 4)
	}

	status, err := statusDB.ApplyDesireStatus().Get(ctx, docID)
	if err != nil {
		if database.IsNotFoundError(err) {
			fmt.Printf("  Status:       (no status document yet)\n")
			return nil
		}
		return fmt.Errorf("getting ApplyDesire status: %w", err)
	}
	printConditions(status.Status.Conditions)
	return nil
}

func getDelete(ctx context.Context, specsDB, statusDB database.KubeApplierDBClient, docID string) error {
	spec, err := specsDB.DeleteDesireSpecs().Get(ctx, docID)
	if err != nil {
		return fmt.Errorf("getting DeleteDesire spec: %w", err)
	}
	printDesireHeader("DeleteDesire", spec.GetDocumentID(), spec.GetUpdateTime().String(), spec.GetCreateTime().String())
	fmt.Printf("  Cluster ID:   %s\n", spec.Spec.ClusterID)
	if spec.Spec.NodePoolName != "" {
		fmt.Printf("  Node Pool:    %s\n", spec.Spec.NodePoolName)
	}
	printTargetItem(spec.Spec.TargetItem)

	status, err := statusDB.DeleteDesireStatus().Get(ctx, docID)
	if err != nil {
		if database.IsNotFoundError(err) {
			fmt.Printf("  Status:       (no status document yet)\n")
			return nil
		}
		return fmt.Errorf("getting DeleteDesire status: %w", err)
	}
	printConditions(status.Status.Conditions)
	return nil
}

func getRead(ctx context.Context, specsDB, statusDB database.KubeApplierDBClient, docID string) error {
	spec, err := specsDB.ReadDesireSpecs().Get(ctx, docID)
	if err != nil {
		return fmt.Errorf("getting ReadDesire spec: %w", err)
	}
	printDesireHeader("ReadDesire", spec.GetDocumentID(), spec.GetUpdateTime().String(), spec.GetCreateTime().String())
	fmt.Printf("  Cluster ID:   %s\n", spec.Spec.ClusterID)
	if spec.Spec.NodePoolName != "" {
		fmt.Printf("  Node Pool:    %s\n", spec.Spec.NodePoolName)
	}
	printTargetItem(spec.Spec.TargetItem)

	status, err := statusDB.ReadDesireStatus().Get(ctx, docID)
	if err != nil {
		if database.IsNotFoundError(err) {
			fmt.Printf("  Status:       (no status document yet)\n")
			return nil
		}
		return fmt.Errorf("getting ReadDesire status: %w", err)
	}
	printConditions(status.Status.Conditions)
	if status.Status.KubeContent != nil && len(status.Status.KubeContent.Raw) > 0 {
		fmt.Printf("  KubeContent (observed):\n")
		printIndentedJSON(status.Status.KubeContent.Raw, 4)
	}
	return nil
}

// --- list ---

func newListApplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-apply",
		Short: "List all ApplyDesires (from specs database)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			specsDB, _, cleanup := twoDBClients(ctx)
			defer cleanup()
			return listApply(ctx, specsDB)
		},
	}
}

func newListDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-delete",
		Short: "List all DeleteDesires (from specs database)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			specsDB, _, cleanup := twoDBClients(ctx)
			defer cleanup()
			return listDelete(ctx, specsDB)
		},
	}
}

func newListReadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-read",
		Short: "List all ReadDesires (from specs database)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			specsDB, _, cleanup := twoDBClients(ctx)
			defer cleanup()
			return listRead(ctx, specsDB)
		},
	}
}

func listApply(ctx context.Context, specsDB database.KubeApplierDBClient) error {
	desires, err := specsDB.ApplyDesireSpecs().List(ctx)
	if err != nil {
		return fmt.Errorf("listing ApplyDesires: %w", err)
	}
	if len(desires) == 0 {
		fmt.Println("No ApplyDesires found.")
		return nil
	}
	fmt.Printf("%-40s %-30s\n", "DOCUMENT ID", "TARGET")
	for _, d := range desires {
		target := fmt.Sprintf("%s/%s %s/%s", d.Spec.TargetItem.Version, d.Spec.TargetItem.Resource, d.Spec.TargetItem.Namespace, d.Spec.TargetItem.Name)
		fmt.Printf("%-40s %-30s\n", d.GetDocumentID(), target)
	}
	return nil
}

func listDelete(ctx context.Context, specsDB database.KubeApplierDBClient) error {
	desires, err := specsDB.DeleteDesireSpecs().List(ctx)
	if err != nil {
		return fmt.Errorf("listing DeleteDesires: %w", err)
	}
	if len(desires) == 0 {
		fmt.Println("No DeleteDesires found.")
		return nil
	}
	fmt.Printf("%-40s %-30s\n", "DOCUMENT ID", "TARGET")
	for _, d := range desires {
		target := fmt.Sprintf("%s/%s %s/%s", d.Spec.TargetItem.Version, d.Spec.TargetItem.Resource, d.Spec.TargetItem.Namespace, d.Spec.TargetItem.Name)
		fmt.Printf("%-40s %-30s\n", d.GetDocumentID(), target)
	}
	return nil
}

func listRead(ctx context.Context, specsDB database.KubeApplierDBClient) error {
	desires, err := specsDB.ReadDesireSpecs().List(ctx)
	if err != nil {
		return fmt.Errorf("listing ReadDesires: %w", err)
	}
	if len(desires) == 0 {
		fmt.Println("No ReadDesires found.")
		return nil
	}
	fmt.Printf("%-40s %-30s\n", "DOCUMENT ID", "TARGET")
	for _, d := range desires {
		target := fmt.Sprintf("%s/%s %s/%s", d.Spec.TargetItem.Version, d.Spec.TargetItem.Resource, d.Spec.TargetItem.Namespace, d.Spec.TargetItem.Name)
		fmt.Printf("%-40s %-30s\n", d.GetDocumentID(), target)
	}
	return nil
}

// --- delete ---

func newDeleteApplyCmd() *cobra.Command {
	var docID string

	cmd := &cobra.Command{
		Use:   "delete-apply",
		Short: "Delete an ApplyDesire document from both specs and status databases",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			specsDB, statusDB, cleanup := twoDBClients(ctx)
			defer cleanup()

			var errs []string
			if err := specsDB.ApplyDesireStatus().Delete(ctx, docID); err != nil {
				if !database.IsNotFoundError(err) {
					errs = append(errs, fmt.Sprintf("specs-db: %v", err))
				}
			}
			if err := statusDB.ApplyDesireStatus().Delete(ctx, docID); err != nil {
				if !database.IsNotFoundError(err) {
					errs = append(errs, fmt.Sprintf("status-db: %v", err))
				}
			}
			if len(errs) > 0 {
				return fmt.Errorf("deleting ApplyDesire: %s", strings.Join(errs, "; "))
			}
			fmt.Printf("Deleted ApplyDesire %s\n", docID)
			return nil
		},
	}

	cmd.Flags().StringVar(&docID, "doc-id", "", "Document ID (required)")
	cmd.MarkFlagRequired("doc-id")

	return cmd
}

func newDeleteDeleteCmd() *cobra.Command {
	var docID string

	cmd := &cobra.Command{
		Use:   "delete-delete",
		Short: "Delete a DeleteDesire document from both specs and status databases",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			specsDB, statusDB, cleanup := twoDBClients(ctx)
			defer cleanup()

			var errs []string
			if err := specsDB.DeleteDesireStatus().Delete(ctx, docID); err != nil {
				if !database.IsNotFoundError(err) {
					errs = append(errs, fmt.Sprintf("specs-db: %v", err))
				}
			}
			if err := statusDB.DeleteDesireStatus().Delete(ctx, docID); err != nil {
				if !database.IsNotFoundError(err) {
					errs = append(errs, fmt.Sprintf("status-db: %v", err))
				}
			}
			if len(errs) > 0 {
				return fmt.Errorf("deleting DeleteDesire: %s", strings.Join(errs, "; "))
			}
			fmt.Printf("Deleted DeleteDesire %s\n", docID)
			return nil
		},
	}

	cmd.Flags().StringVar(&docID, "doc-id", "", "Document ID (required)")
	cmd.MarkFlagRequired("doc-id")

	return cmd
}

func newDeleteReadCmd() *cobra.Command {
	var docID string

	cmd := &cobra.Command{
		Use:   "delete-read",
		Short: "Delete a ReadDesire document from both specs and status databases",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			specsDB, statusDB, cleanup := twoDBClients(ctx)
			defer cleanup()

			var errs []string
			if err := specsDB.ReadDesireStatus().Delete(ctx, docID); err != nil {
				if !database.IsNotFoundError(err) {
					errs = append(errs, fmt.Sprintf("specs-db: %v", err))
				}
			}
			if err := statusDB.ReadDesireStatus().Delete(ctx, docID); err != nil {
				if !database.IsNotFoundError(err) {
					errs = append(errs, fmt.Sprintf("status-db: %v", err))
				}
			}
			if len(errs) > 0 {
				return fmt.Errorf("deleting ReadDesire: %s", strings.Join(errs, "; "))
			}
			fmt.Printf("Deleted ReadDesire %s\n", docID)
			return nil
		},
	}

	cmd.Flags().StringVar(&docID, "doc-id", "", "Document ID (required)")
	cmd.MarkFlagRequired("doc-id")

	return cmd
}

// --- helpers ---

func resourceRefFromManifest(raw []byte) (kubeapplier.ResourceReference, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return kubeapplier.ResourceReference{}, fmt.Errorf("manifest must be JSON: %w", err)
	}

	apiVersion, _ := obj["apiVersion"].(string)
	kind, _ := obj["kind"].(string)
	metadata, _ := obj["metadata"].(map[string]any)

	if apiVersion == "" || kind == "" {
		return kubeapplier.ResourceReference{}, fmt.Errorf("manifest must have apiVersion and kind")
	}

	ref := kubeapplier.ResourceReference{
		Name: strFromMap(metadata, "name"),
	}
	if ns := strFromMap(metadata, "namespace"); ns != "" {
		ref.Namespace = ns
	}

	parts := strings.SplitN(apiVersion, "/", 2)
	if len(parts) == 2 {
		ref.Group = parts[0]
		ref.Version = parts[1]
	} else {
		ref.Version = apiVersion
	}

	ref.Resource = guessResource(kind)

	return ref, nil
}

func strFromMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

func guessResource(kind string) string {
	switch kind {
	case "ConfigMap":
		return "configmaps"
	case "Secret":
		return "secrets"
	case "Namespace":
		return "namespaces"
	case "Service":
		return "services"
	case "ServiceAccount":
		return "serviceaccounts"
	case "Deployment":
		return "deployments"
	case "StatefulSet":
		return "statefulsets"
	case "DaemonSet":
		return "daemonsets"
	case "Job":
		return "jobs"
	case "CronJob":
		return "cronjobs"
	case "Pod":
		return "pods"
	case "ClusterRole":
		return "clusterroles"
	case "ClusterRoleBinding":
		return "clusterrolebindings"
	case "Role":
		return "roles"
	case "RoleBinding":
		return "rolebindings"
	default:
		return fmt.Sprintf("%ss", toLowerFirst(kind))
	}
}

func toLowerFirst(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if b[0] >= 'A' && b[0] <= 'Z' {
		b[0] += 'a' - 'A'
	}
	return string(b)
}

func conditionSummary(conditions []metav1.Condition) (string, string) {
	for _, c := range conditions {
		if c.Type == kubeapplier.ConditionTypeSuccessful {
			if c.Status == metav1.ConditionTrue {
				return "True", ""
			}
			return fmt.Sprintf("False/%s", c.Reason), c.Message
		}
	}
	return "Unknown", ""
}

func printDesireHeader(kind, docID, updateTime, createTime string) {
	fmt.Printf("%s: %s\n", kind, docID)
	fmt.Printf("  Created:      %s\n", createTime)
	fmt.Printf("  Updated:      %s\n", updateTime)
}

func printTargetItem(ref kubeapplier.ResourceReference) {
	fmt.Printf("  Target Item:\n")
	if ref.Group != "" {
		fmt.Printf("    Group:      %s\n", ref.Group)
	}
	fmt.Printf("    Version:    %s\n", ref.Version)
	fmt.Printf("    Resource:   %s\n", ref.Resource)
	if ref.Namespace != "" {
		fmt.Printf("    Namespace:  %s\n", ref.Namespace)
	}
	fmt.Printf("    Name:       %s\n", ref.Name)
}

func printConditions(conditions []metav1.Condition) {
	if len(conditions) == 0 {
		fmt.Printf("  Conditions:   (none)\n")
		return
	}
	fmt.Printf("  Conditions:\n")
	for _, c := range conditions {
		fmt.Printf("    - Type: %s, Status: %s, Reason: %s\n", c.Type, string(c.Status), c.Reason)
		if c.Message != "" {
			fmt.Printf("      Message: %s\n", c.Message)
		}
		if !c.LastTransitionTime.IsZero() {
			fmt.Printf("      LastTransition: %s\n", c.LastTransitionTime.Time.String())
		}
	}
}

func printIndentedJSON(raw []byte, indent int) {
	var obj any
	if err := json.Unmarshal(raw, &obj); err != nil {
		fmt.Printf("%*s%s\n", indent, "", string(raw))
		return
	}
	formatted, err := json.MarshalIndent(obj, fmt.Sprintf("%*s", indent, ""), "  ")
	if err != nil {
		fmt.Printf("%*s%s\n", indent, "", string(raw))
		return
	}
	fmt.Printf("%*s%s\n", indent, "", string(formatted))
}
