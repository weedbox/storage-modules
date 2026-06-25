# storage_connector

The shared storage contract for [`weedbox/storage-modules`](https://github.com/weedbox/storage-modules). It defines a single backend-agnostic interface, `StorageConnector`, that every storage backend implements (`s3_connector`, `local_storage_connector`, …).

Depending on this interface — instead of a concrete connector — lets an application swap S3 for the local filesystem (or any future backend) without touching consumer code.

## Import

```go
import "github.com/weedbox/storage-modules/storage_connector"
```

## The interface

```go
type StorageConnector interface {
    // WriteAsFile writes raw bytes to filePath and returns its access URL.
    WriteAsFile(filePath string, content []byte) (string, error)

    // SaveFile decodes base64 content and stores it under {category}/{filename},
    // generating a UUID filename when FileName is empty. Returns the access URL.
    SaveFile(req *UploaderReq) (string, error)

    // ReadFile returns the full content of the object at filePath.
    ReadFile(filePath string) ([]byte, error)

    // Exists reports whether an object exists at filePath.
    Exists(filePath string) (bool, error)

    // DeleteFile removes a single object. Missing objects are not an error.
    DeleteFile(filePath string) error

    // DeleteFileWithPrefix removes every object whose key starts with prefix.
    DeleteFileWithPrefix(prefix string) error

    // PublicURL builds the (unsigned) access URL for a key.
    PublicURL(filePath string) string
}
```

Backend-specific capabilities that don't generalize stay **off** the interface and on the concrete type. For example, presigned URLs and the raw SDK client live on `*s3_connector.S3Connector`; reach them by type-asserting the injected interface:

```go
if s3c, ok := sc.(*s3_connector.S3Connector); ok {
    link, _ := s3c.PresignGetURL("avatars/profile.jpg", 10*time.Minute)
}
```

## UploaderReq

```go
type UploaderReq struct {
    FileName string `json:"file_name"` // Target filename (optional, auto-generates UUID if empty)
    Category string `json:"category"`  // Category/directory path (key prefix)
    RawData  string `json:"rowData"`   // Base64-encoded file content
}
```

> The JSON tag `rowData` (not `rawData`) is kept identical to `weedbox/gcp-modules` for drop-in request compatibility.

## Helpers

```go
// DetectContentType maps a file extension to a MIME type, falling back to
// "application/octet-stream". Used by backends to set Content-Type on upload.
func DetectContentType(filePath string) string
```

## Using the interface

Wire any backend's `Module(scope)`; both provide `storage_connector.StorageConnector`. Write consumers against the interface:

```go
type Params struct {
    fx.In
    Storage storage_connector.StorageConnector
}

func NewService(p Params) *Service {
    return &Service{storage: p.Storage}
}
```

```go
app := fx.New(
    fx.Provide(zap.NewDevelopment),

    // Pick a backend — consumers don't change either way:
    s3_connector.Module("storage"),
    // local_storage_connector.Module("storage"),

    fx.Provide(NewService),
)
```

### Running multiple backends side by side

The underlying registration ([`fxmodule.InterfaceModule`](https://github.com/weedbox/weedbox)) claims the unnamed default for the first loaded backend and also registers each under `name:"<scope>"`. To load more than one, inject the named results:

```go
app := fx.New(
    fx.Provide(zap.NewDevelopment),
    s3_connector.Module("s3"),
    local_storage_connector.Module("local"),
    fx.Invoke(func(in struct {
        fx.In
        Remote storage_connector.StorageConnector `name:"s3"`
        Local  storage_connector.StorageConnector `name:"local"`
    }) {
        // in.Remote -> S3, in.Local -> filesystem
    }),
)
```

## Implementations

| Backend | Package | Notes |
|---------|---------|-------|
| AWS S3 / S3-compatible (R2, MinIO) | [`s3_connector`](../s3_connector) | Adds presigned URLs and raw SDK access |
| Local filesystem | [`local_storage_connector`](../local_storage_connector) | Great for development and tests |
