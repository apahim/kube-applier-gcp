package kubeapplier

import "time"

// FirestoreMetadata holds the server-managed fields extracted from a Firestore
// DocumentSnapshot. All fields carry firestore:"-" because they are not stored
// in the document body — the CRUD layer populates them from snap.Ref.ID,
// snap.UpdateTime, and snap.CreateTime.
type FirestoreMetadata struct {
	DocumentID string    `json:"documentID" firestore:"-"`
	UpdateTime time.Time `json:"updateTime" firestore:"-"`
	CreateTime time.Time `json:"createTime,omitempty" firestore:"-"`
}

func (m *FirestoreMetadata) GetDocumentID() string    { return m.DocumentID }
func (m *FirestoreMetadata) GetUpdateTime() time.Time { return m.UpdateTime }
func (m *FirestoreMetadata) GetCreateTime() time.Time { return m.CreateTime }
