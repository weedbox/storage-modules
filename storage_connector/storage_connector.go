// Package storage_connector defines the shared contract implemented by every
// storage backend in storage-modules (s3_connector, local_storage_connector,
// ...). Backends register themselves as implementations of StorageConnector via
// github.com/weedbox/weedbox/fxmodule.InterfaceModule, so an application can
// inject the interface and swap or load multiple backends side by side.
package storage_connector

import (
	"mime"
	"path/filepath"
)

// StorageConnector is the backend-agnostic object-storage interface. It covers
// the operations every backend can implement cleanly. Backend-specific features
// (e.g. S3 presigned URLs) live on the concrete connector type and are reached
// via named injection or a type assertion.
type StorageConnector interface {
	// WriteAsFile writes raw bytes to filePath and returns its access URL.
	WriteAsFile(filePath string, content []byte) (string, error)

	// SaveFile decodes base64 content and stores it under {Category}/{FileName}
	// (a UUID is generated when FileName is empty). Returns the access URL.
	SaveFile(req *UploaderReq) (string, error)

	// ReadFile returns the full content of the object at filePath.
	ReadFile(filePath string) ([]byte, error)

	// Exists reports whether an object exists at filePath.
	Exists(filePath string) (bool, error)

	// DeleteFile removes a single object. Implementations are idempotent: a
	// missing object is not treated as an error.
	DeleteFile(filePath string) error

	// DeleteFileWithPrefix removes every object whose key starts with prefix.
	DeleteFileWithPrefix(prefix string) error

	// PublicURL builds the public (unsigned) URL for a key. Whether the URL is
	// actually reachable depends on the backend's configuration.
	PublicURL(filePath string) string
}

// UploaderReq is the request payload accepted by SaveFile. It is shared by all
// backends and mirrors the weedbox/gcp-modules bucket_connector contract so
// callers can switch backends without changing their request shape.
type UploaderReq struct {
	FileName string `json:"file_name"` // Target filename (optional, auto-generates UUID if empty)
	Category string `json:"category"`  // Object key prefix (directory) in the store
	RawData  string `json:"rowData"`   // Base64-encoded file content
}

// DetectContentType returns a MIME type guessed from the file extension,
// falling back to application/octet-stream.
func DetectContentType(filePath string) string {
	if ct := mime.TypeByExtension(filepath.Ext(filePath)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}
