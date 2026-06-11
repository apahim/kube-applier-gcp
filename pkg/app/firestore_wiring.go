package app

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
)

// NewFirestoreClient creates a Firestore client scoped to the given named
// database. On GKE, Workload Identity Federation supplies credentials
// automatically via Application Default Credentials.
func NewFirestoreClient(ctx context.Context, projectID, databaseID string) (*firestore.Client, error) {
	client, err := firestore.NewClientWithDatabase(ctx, projectID, databaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to create Firestore client for database %s: %w", databaseID, err)
	}
	return client, nil
}
