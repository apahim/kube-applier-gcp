package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"cloud.google.com/go/firestore"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	sigsyaml "sigs.k8s.io/yaml"

	kubeapplier "github.com/openshift/kube-applier-gcp/internal/api/kubeapplier"
	"github.com/openshift/kube-applier-gcp/internal/database"
)

var (
	flagProject  string
	flagDatabase string
)

func main() {
	root := &cobra.Command{
		Use:   "desire-tool",
		Short: "CLI for creating and inspecting kube-applier desires in Firestore",
	}

	root.PersistentFlags().StringVar(&flagProject, "project", "test-project", "GCP project ID")
	root.PersistentFlags().StringVar(&flagDatabase, "database", "mc-dev-local", "Firestore named database ID")

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

func newClient(ctx context.Context) (database.KubeApplierDBClient, func()) {
	client, err := firestore.NewClientWithDatabase(ctx, flagProject, flagDatabase)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to connect to Firestore: %v\n", err)
		os.Exit(1)
	}
	dbClient := database.NewFirestoreKubeApplierDBClient(client)
	return dbClient, func() { dbClient.Close() }
}

func documentID(clusterID, name string) string {
	return clusterID + "--" + name
}

// --- create ---

func newCreateApplyCmd() *cobra.Command {
	var clusterID, name, nodePool, filePath string

	cmd := &cobra.Command{
		Use:   "create-apply",
		Short: "Create an ApplyDesire from a YAML/JSON manifest file",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			dbClient, cleanup := newClient(ctx)
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

			desire := &kubeapplier.ApplyDesire{
				Spec: kubeapplier.ApplyDesireSpec{
					ManagementCluster: "dev-local",
					ClusterID:         clusterID,
					NodePoolName:      nodePool,
					TargetItem:        ref,
					KubeContent:       &runtime.RawExtension{Raw: raw},
				},
			}
			desire.SetDocumentID(documentID(clusterID, name))

			created, err := dbClient.ApplyDesires().Create(ctx, desire)
			if err != nil {
				return fmt.Errorf("creating ApplyDesire: %w", err)
			}

			fmt.Printf("Created ApplyDesire: %s\n", created.GetDocumentID())
			fmt.Printf("  Target: %s/%s %s/%s\n", ref.Version, ref.Resource, ref.Namespace, ref.Name)
			return nil
		},
	}

	cmd.Flags().StringVar(&clusterID, "cluster-id", "", "Cluster ID (required)")
	cmd.Flags().StringVar(&name, "name", "", "Desire name (required)")
	cmd.Flags().StringVar(&nodePool, "node-pool", "", "Node pool name (optional)")
	cmd.Flags().StringVar(&filePath, "file", "", "Path to YAML/JSON manifest (required)")
	cmd.MarkFlagRequired("cluster-id")
	cmd.MarkFlagRequired("name")
	cmd.MarkFlagRequired("file")

	return cmd
}

func newCreateDeleteCmd() *cobra.Command {
	var clusterID, name, nodePool string
	var group, version, resource, namespace, resourceName string

	cmd := &cobra.Command{
		Use:   "create-delete",
		Short: "Create a DeleteDesire targeting a specific resource",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			dbClient, cleanup := newClient(ctx)
			defer cleanup()

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
			desire.SetDocumentID(documentID(clusterID, name))

			created, err := dbClient.DeleteDesires().Create(ctx, desire)
			if err != nil {
				return fmt.Errorf("creating DeleteDesire: %w", err)
			}

			fmt.Printf("Created DeleteDesire: %s\n", created.GetDocumentID())
			fmt.Printf("  Target: %s/%s %s/%s\n", version, resource, namespace, resourceName)
			return nil
		},
	}

	cmd.Flags().StringVar(&clusterID, "cluster-id", "", "Cluster ID (required)")
	cmd.Flags().StringVar(&name, "name", "", "Desire name (required)")
	cmd.Flags().StringVar(&nodePool, "node-pool", "", "Node pool name (optional)")
	cmd.Flags().StringVar(&group, "group", "", "API group (empty for core)")
	cmd.Flags().StringVar(&version, "version", "", "API version (required)")
	cmd.Flags().StringVar(&resource, "resource", "", "Resource type (required)")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Namespace (empty for cluster-scoped)")
	cmd.Flags().StringVar(&resourceName, "resource-name", "", "Resource name (required)")
	cmd.MarkFlagRequired("cluster-id")
	cmd.MarkFlagRequired("name")
	cmd.MarkFlagRequired("version")
	cmd.MarkFlagRequired("resource")
	cmd.MarkFlagRequired("resource-name")

	return cmd
}

func newCreateReadCmd() *cobra.Command {
	var clusterID, name, nodePool string
	var group, version, resource, namespace, resourceName string

	cmd := &cobra.Command{
		Use:   "create-read",
		Short: "Create a ReadDesire targeting a specific resource",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			dbClient, cleanup := newClient(ctx)
			defer cleanup()

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
			desire.SetDocumentID(documentID(clusterID, name))

			created, err := dbClient.ReadDesires().Create(ctx, desire)
			if err != nil {
				return fmt.Errorf("creating ReadDesire: %w", err)
			}

			fmt.Printf("Created ReadDesire: %s\n", created.GetDocumentID())
			fmt.Printf("  Target: %s/%s %s/%s\n", version, resource, namespace, resourceName)
			return nil
		},
	}

	cmd.Flags().StringVar(&clusterID, "cluster-id", "", "Cluster ID (required)")
	cmd.Flags().StringVar(&name, "name", "", "Desire name (required)")
	cmd.Flags().StringVar(&nodePool, "node-pool", "", "Node pool name (optional)")
	cmd.Flags().StringVar(&group, "group", "", "API group (empty for core)")
	cmd.Flags().StringVar(&version, "version", "", "API version (required)")
	cmd.Flags().StringVar(&resource, "resource", "", "Resource type (required)")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Namespace (empty for cluster-scoped)")
	cmd.Flags().StringVar(&resourceName, "resource-name", "", "Resource name (required)")
	cmd.MarkFlagRequired("cluster-id")
	cmd.MarkFlagRequired("name")
	cmd.MarkFlagRequired("version")
	cmd.MarkFlagRequired("resource")
	cmd.MarkFlagRequired("resource-name")

	return cmd
}

// --- update ---

func newUpdateApplyCmd() *cobra.Command {
	var clusterID, name, filePath string

	cmd := &cobra.Command{
		Use:   "update-apply",
		Short: "Update an existing ApplyDesire's manifest (read-modify-write with optimistic concurrency)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			dbClient, cleanup := newClient(ctx)
			defer cleanup()

			docID := documentID(clusterID, name)

			existing, err := dbClient.ApplyDesires().Get(ctx, docID)
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

			updated, err := dbClient.ApplyDesires().Replace(ctx, existing)
			if err != nil {
				return fmt.Errorf("updating ApplyDesire: %w", err)
			}

			fmt.Printf("Updated ApplyDesire: %s\n", updated.GetDocumentID())
			fmt.Printf("  Target: %s/%s %s/%s\n", ref.Version, ref.Resource, ref.Namespace, ref.Name)
			return nil
		},
	}

	cmd.Flags().StringVar(&clusterID, "cluster-id", "", "Cluster ID (required)")
	cmd.Flags().StringVar(&name, "name", "", "Desire name (required)")
	cmd.Flags().StringVar(&filePath, "file", "", "Path to updated YAML/JSON manifest (required)")
	cmd.MarkFlagRequired("cluster-id")
	cmd.MarkFlagRequired("name")
	cmd.MarkFlagRequired("file")

	return cmd
}

func newUpdateReadCmd() *cobra.Command {
	var clusterID, name string
	var group, version, resource, namespace, resourceName string

	cmd := &cobra.Command{
		Use:   "update-read",
		Short: "Update an existing ReadDesire's target (read-modify-write with optimistic concurrency)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			dbClient, cleanup := newClient(ctx)
			defer cleanup()

			docID := documentID(clusterID, name)

			existing, err := dbClient.ReadDesires().Get(ctx, docID)
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

			updated, err := dbClient.ReadDesires().Replace(ctx, existing)
			if err != nil {
				return fmt.Errorf("updating ReadDesire: %w", err)
			}

			fmt.Printf("Updated ReadDesire: %s\n", updated.GetDocumentID())
			fmt.Printf("  Target: %s/%s %s/%s\n", version, resource, namespace, resourceName)
			return nil
		},
	}

	cmd.Flags().StringVar(&clusterID, "cluster-id", "", "Cluster ID (required)")
	cmd.Flags().StringVar(&name, "name", "", "Desire name (required)")
	cmd.Flags().StringVar(&group, "group", "", "API group (empty for core)")
	cmd.Flags().StringVar(&version, "version", "", "API version (required)")
	cmd.Flags().StringVar(&resource, "resource", "", "Resource type (required)")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Namespace (empty for cluster-scoped)")
	cmd.Flags().StringVar(&resourceName, "resource-name", "", "Resource name (required)")
	cmd.MarkFlagRequired("cluster-id")
	cmd.MarkFlagRequired("name")
	cmd.MarkFlagRequired("version")
	cmd.MarkFlagRequired("resource")
	cmd.MarkFlagRequired("resource-name")

	return cmd
}

// --- get ---

func newGetApplyCmd() *cobra.Command {
	var clusterID, name string

	cmd := &cobra.Command{
		Use:   "get-apply",
		Short: "Get a single ApplyDesire with full details",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			dbClient, cleanup := newClient(ctx)
			defer cleanup()
			return getApply(ctx, dbClient, documentID(clusterID, name))
		},
	}

	cmd.Flags().StringVar(&clusterID, "cluster-id", "", "Cluster ID (required)")
	cmd.Flags().StringVar(&name, "name", "", "Desire name (required)")
	cmd.MarkFlagRequired("cluster-id")
	cmd.MarkFlagRequired("name")

	return cmd
}

func newGetDeleteCmd() *cobra.Command {
	var clusterID, name string

	cmd := &cobra.Command{
		Use:   "get-delete",
		Short: "Get a single DeleteDesire with full details",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			dbClient, cleanup := newClient(ctx)
			defer cleanup()
			return getDelete(ctx, dbClient, documentID(clusterID, name))
		},
	}

	cmd.Flags().StringVar(&clusterID, "cluster-id", "", "Cluster ID (required)")
	cmd.Flags().StringVar(&name, "name", "", "Desire name (required)")
	cmd.MarkFlagRequired("cluster-id")
	cmd.MarkFlagRequired("name")

	return cmd
}

func newGetReadCmd() *cobra.Command {
	var clusterID, name string

	cmd := &cobra.Command{
		Use:   "get-read",
		Short: "Get a single ReadDesire with full details",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			dbClient, cleanup := newClient(ctx)
			defer cleanup()
			return getRead(ctx, dbClient, documentID(clusterID, name))
		},
	}

	cmd.Flags().StringVar(&clusterID, "cluster-id", "", "Cluster ID (required)")
	cmd.Flags().StringVar(&name, "name", "", "Desire name (required)")
	cmd.MarkFlagRequired("cluster-id")
	cmd.MarkFlagRequired("name")

	return cmd
}

func getApply(ctx context.Context, dbClient database.KubeApplierDBClient, docID string) error {
	d, err := dbClient.ApplyDesires().Get(ctx, docID)
	if err != nil {
		return fmt.Errorf("getting ApplyDesire: %w", err)
	}
	printDesireHeader("ApplyDesire", d.GetDocumentID(), d.GetUpdateTime().String(), d.GetCreateTime().String())
	fmt.Printf("  Cluster ID:   %s\n", d.Spec.ClusterID)
	if d.Spec.NodePoolName != "" {
		fmt.Printf("  Node Pool:    %s\n", d.Spec.NodePoolName)
	}
	printTargetItem(d.Spec.TargetItem)
	if d.Spec.KubeContent != nil {
		fmt.Printf("  KubeContent:\n")
		printIndentedJSON(d.Spec.KubeContent.Raw, 4)
	}
	printConditions(d.Status.Conditions)
	return nil
}

func getDelete(ctx context.Context, dbClient database.KubeApplierDBClient, docID string) error {
	d, err := dbClient.DeleteDesires().Get(ctx, docID)
	if err != nil {
		return fmt.Errorf("getting DeleteDesire: %w", err)
	}
	printDesireHeader("DeleteDesire", d.GetDocumentID(), d.GetUpdateTime().String(), d.GetCreateTime().String())
	fmt.Printf("  Cluster ID:   %s\n", d.Spec.ClusterID)
	if d.Spec.NodePoolName != "" {
		fmt.Printf("  Node Pool:    %s\n", d.Spec.NodePoolName)
	}
	printTargetItem(d.Spec.TargetItem)
	printConditions(d.Status.Conditions)
	return nil
}

func getRead(ctx context.Context, dbClient database.KubeApplierDBClient, docID string) error {
	d, err := dbClient.ReadDesires().Get(ctx, docID)
	if err != nil {
		return fmt.Errorf("getting ReadDesire: %w", err)
	}
	printDesireHeader("ReadDesire", d.GetDocumentID(), d.GetUpdateTime().String(), d.GetCreateTime().String())
	fmt.Printf("  Cluster ID:   %s\n", d.Spec.ClusterID)
	if d.Spec.NodePoolName != "" {
		fmt.Printf("  Node Pool:    %s\n", d.Spec.NodePoolName)
	}
	printTargetItem(d.Spec.TargetItem)
	printConditions(d.Status.Conditions)
	if d.Status.KubeContent != nil && len(d.Status.KubeContent.Raw) > 0 {
		fmt.Printf("  KubeContent (observed):\n")
		printIndentedJSON(d.Status.KubeContent.Raw, 4)
	}
	return nil
}

// --- list ---

func newListApplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-apply",
		Short: "List all ApplyDesires",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			dbClient, cleanup := newClient(ctx)
			defer cleanup()
			return listApply(ctx, dbClient)
		},
	}
}

func newListDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-delete",
		Short: "List all DeleteDesires",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			dbClient, cleanup := newClient(ctx)
			defer cleanup()
			return listDelete(ctx, dbClient)
		},
	}
}

func newListReadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-read",
		Short: "List all ReadDesires",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			dbClient, cleanup := newClient(ctx)
			defer cleanup()
			return listRead(ctx, dbClient)
		},
	}
}

func listApply(ctx context.Context, dbClient database.KubeApplierDBClient) error {
	desires, err := dbClient.ApplyDesires().List(ctx)
	if err != nil {
		return fmt.Errorf("listing ApplyDesires: %w", err)
	}
	if len(desires) == 0 {
		fmt.Println("No ApplyDesires found.")
		return nil
	}
	fmt.Printf("%-40s %-12s %-30s %s\n", "DOCUMENT ID", "STATUS", "TARGET", "MESSAGE")
	for _, d := range desires {
		status, msg := conditionSummary(d.Status.Conditions)
		target := fmt.Sprintf("%s/%s %s/%s", d.Spec.TargetItem.Version, d.Spec.TargetItem.Resource, d.Spec.TargetItem.Namespace, d.Spec.TargetItem.Name)
		fmt.Printf("%-40s %-12s %-30s %s\n", d.GetDocumentID(), status, target, truncate(msg, 60))
	}
	return nil
}

func listDelete(ctx context.Context, dbClient database.KubeApplierDBClient) error {
	desires, err := dbClient.DeleteDesires().List(ctx)
	if err != nil {
		return fmt.Errorf("listing DeleteDesires: %w", err)
	}
	if len(desires) == 0 {
		fmt.Println("No DeleteDesires found.")
		return nil
	}
	fmt.Printf("%-40s %-12s %-30s %s\n", "DOCUMENT ID", "STATUS", "TARGET", "MESSAGE")
	for _, d := range desires {
		status, msg := conditionSummary(d.Status.Conditions)
		target := fmt.Sprintf("%s/%s %s/%s", d.Spec.TargetItem.Version, d.Spec.TargetItem.Resource, d.Spec.TargetItem.Namespace, d.Spec.TargetItem.Name)
		fmt.Printf("%-40s %-12s %-30s %s\n", d.GetDocumentID(), status, target, truncate(msg, 60))
	}
	return nil
}

func listRead(ctx context.Context, dbClient database.KubeApplierDBClient) error {
	desires, err := dbClient.ReadDesires().List(ctx)
	if err != nil {
		return fmt.Errorf("listing ReadDesires: %w", err)
	}
	if len(desires) == 0 {
		fmt.Println("No ReadDesires found.")
		return nil
	}
	fmt.Printf("%-40s %-12s %-30s %s\n", "DOCUMENT ID", "STATUS", "TARGET", "HAS CONTENT")
	for _, d := range desires {
		status, _ := conditionSummary(d.Status.Conditions)
		target := fmt.Sprintf("%s/%s %s/%s", d.Spec.TargetItem.Version, d.Spec.TargetItem.Resource, d.Spec.TargetItem.Namespace, d.Spec.TargetItem.Name)
		hasContent := "no"
		if d.Status.KubeContent != nil && len(d.Status.KubeContent.Raw) > 0 {
			hasContent = "yes"
		}
		fmt.Printf("%-40s %-12s %-30s %s\n", d.GetDocumentID(), status, target, hasContent)
	}
	return nil
}

// --- delete ---

func newDeleteApplyCmd() *cobra.Command {
	var clusterID, name string

	cmd := &cobra.Command{
		Use:   "delete-apply",
		Short: "Delete an ApplyDesire document from Firestore",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			dbClient, cleanup := newClient(ctx)
			defer cleanup()

			docID := documentID(clusterID, name)
			if err := dbClient.ApplyDesires().Delete(ctx, docID); err != nil {
				return fmt.Errorf("deleting ApplyDesire: %w", err)
			}
			fmt.Printf("Deleted ApplyDesire %s\n", docID)
			return nil
		},
	}

	cmd.Flags().StringVar(&clusterID, "cluster-id", "", "Cluster ID (required)")
	cmd.Flags().StringVar(&name, "name", "", "Desire name (required)")
	cmd.MarkFlagRequired("cluster-id")
	cmd.MarkFlagRequired("name")

	return cmd
}

func newDeleteDeleteCmd() *cobra.Command {
	var clusterID, name string

	cmd := &cobra.Command{
		Use:   "delete-delete",
		Short: "Delete a DeleteDesire document from Firestore",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			dbClient, cleanup := newClient(ctx)
			defer cleanup()

			docID := documentID(clusterID, name)
			if err := dbClient.DeleteDesires().Delete(ctx, docID); err != nil {
				return fmt.Errorf("deleting DeleteDesire: %w", err)
			}
			fmt.Printf("Deleted DeleteDesire %s\n", docID)
			return nil
		},
	}

	cmd.Flags().StringVar(&clusterID, "cluster-id", "", "Cluster ID (required)")
	cmd.Flags().StringVar(&name, "name", "", "Desire name (required)")
	cmd.MarkFlagRequired("cluster-id")
	cmd.MarkFlagRequired("name")

	return cmd
}

func newDeleteReadCmd() *cobra.Command {
	var clusterID, name string

	cmd := &cobra.Command{
		Use:   "delete-read",
		Short: "Delete a ReadDesire document from Firestore",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			dbClient, cleanup := newClient(ctx)
			defer cleanup()

			docID := documentID(clusterID, name)
			if err := dbClient.ReadDesires().Delete(ctx, docID); err != nil {
				return fmt.Errorf("deleting ReadDesire: %w", err)
			}
			fmt.Printf("Deleted ReadDesire %s\n", docID)
			return nil
		},
	}

	cmd.Flags().StringVar(&clusterID, "cluster-id", "", "Cluster ID (required)")
	cmd.Flags().StringVar(&name, "name", "", "Desire name (required)")
	cmd.MarkFlagRequired("cluster-id")
	cmd.MarkFlagRequired("name")

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
		Version: apiVersion,
		Name:    strFromMap(metadata, "name"),
	}
	if ns := strFromMap(metadata, "namespace"); ns != "" {
		ref.Namespace = ns
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

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
