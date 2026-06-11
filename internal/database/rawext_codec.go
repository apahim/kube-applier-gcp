package database

import (
	"encoding/json"
	"fmt"

	"cloud.google.com/go/firestore"
	"k8s.io/apimachinery/pkg/runtime"
)

// rawExtField is a Firestore document field name used to store the manually
// serialized RawExtension content. It lives at the document root level
// alongside "spec" and "status", outside Firestore's automatic struct codec.
const (
	rawExtFieldSpecKubeContent   = "spec_kubeContent"
	rawExtFieldStatusKubeContent = "status_kubeContent"
)

// KubeContentAccessor provides access to the RawExtension fields that are
// tagged firestore:"-" and need manual serialization. Types that carry no
// KubeContent (DeleteDesire) return nil from both getters; the CRUD layer
// skips serialization when both are nil.
type KubeContentAccessor interface {
	GetSpecKubeContent() *runtime.RawExtension
	SetSpecKubeContent(*runtime.RawExtension)
	GetStatusKubeContent() *runtime.RawExtension
	SetStatusKubeContent(*runtime.RawExtension)
}

// rawExtToMap converts a RawExtension's JSON bytes into a map[string]any
// suitable for Firestore storage.
func rawExtToMap(ext *runtime.RawExtension) (map[string]any, error) {
	if ext == nil || len(ext.Raw) == 0 {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal(ext.Raw, &m); err != nil {
		return nil, fmt.Errorf("unmarshal RawExtension: %w", err)
	}
	return m, nil
}

// mapToRawExt converts a Firestore map value back into a RawExtension.
func mapToRawExt(v any) (*runtime.RawExtension, error) {
	if v == nil {
		return nil, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal RawExtension: %w", err)
	}
	return &runtime.RawExtension{Raw: raw}, nil
}

// kubeContentWriteUpdates returns firestore.Update entries for any non-nil
// KubeContent fields. Used by Replace to include RawExtension fields alongside
// the spec/status updates.
func kubeContentWriteUpdates(acc KubeContentAccessor) ([]firestore.Update, error) {
	var updates []firestore.Update
	if ext := acc.GetSpecKubeContent(); ext != nil {
		m, err := rawExtToMap(ext)
		if err != nil {
			return nil, err
		}
		updates = append(updates, firestore.Update{Path: rawExtFieldSpecKubeContent, Value: m})
	} else {
		updates = append(updates, firestore.Update{Path: rawExtFieldSpecKubeContent, Value: firestore.Delete})
	}
	if ext := acc.GetStatusKubeContent(); ext != nil {
		m, err := rawExtToMap(ext)
		if err != nil {
			return nil, err
		}
		updates = append(updates, firestore.Update{Path: rawExtFieldStatusKubeContent, Value: m})
	} else {
		updates = append(updates, firestore.Update{Path: rawExtFieldStatusKubeContent, Value: firestore.Delete})
	}
	return updates, nil
}

// kubeContentWriteMap returns a map of the RawExtension fields for use with
// Create (which writes the full document).
func kubeContentWriteMap(acc KubeContentAccessor) (map[string]any, error) {
	result := make(map[string]any)
	if ext := acc.GetSpecKubeContent(); ext != nil {
		m, err := rawExtToMap(ext)
		if err != nil {
			return nil, err
		}
		result[rawExtFieldSpecKubeContent] = m
	}
	if ext := acc.GetStatusKubeContent(); ext != nil {
		m, err := rawExtToMap(ext)
		if err != nil {
			return nil, err
		}
		result[rawExtFieldStatusKubeContent] = m
	}
	return result, nil
}

// kubeContentReadFromSnapshot reads the manually-stored RawExtension fields
// from a Firestore document snapshot and sets them on the desire object.
func kubeContentReadFromSnapshot(acc KubeContentAccessor, data map[string]any) error {
	if v, ok := data[rawExtFieldSpecKubeContent]; ok {
		ext, err := mapToRawExt(v)
		if err != nil {
			return fmt.Errorf("read %s: %w", rawExtFieldSpecKubeContent, err)
		}
		acc.SetSpecKubeContent(ext)
	}
	if v, ok := data[rawExtFieldStatusKubeContent]; ok {
		ext, err := mapToRawExt(v)
		if err != nil {
			return fmt.Errorf("read %s: %w", rawExtFieldStatusKubeContent, err)
		}
		acc.SetStatusKubeContent(ext)
	}
	return nil
}
