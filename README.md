# storage-modules

Pluggable object-storage modules for [Weedbox](https://github.com/weedbox) applications, built with [Uber Fx](https://github.com/uber-go/fx) dependency injection.

Every backend implements one shared interface — [`storage_connector.StorageConnector`](./storage_connector) — so an application depends on the contract, not on a particular backend, and can swap **AWS S3** for the **local filesystem** (or any future backend) without changing consumer code. The file API mirrors [`weedbox/gcp-modules`](https://github.com/weedbox/gcp-modules), so GCS is interchangeable too.

## Modules

| Module | Provides | Description |
|--------|----------|-------------|
| [`storage_connector`](./storage_connector) | the `StorageConnector` interface | Shared contract every backend implements. Depend on this. |
| [`s3_connector`](./s3_connector) | `StorageConnector` (`*S3Connector`) | AWS S3 / S3-compatible (Cloudflare R2, MinIO). Adds presigned URLs + raw SDK access. Uses [AWS SDK for Go v2](https://github.com/aws/aws-sdk-go-v2). |
| [`local_storage_connector`](./local_storage_connector) | `StorageConnector` (`*LocalStorageConnector`) | Local filesystem backend. Ideal for development, tests, single-node deploys. |

## Installation

```bash
go get github.com/weedbox/storage-modules
```

## Quick Start

Wire any backend's `Module(scope)` and depend on the shared interface:

```go
package main

import (
    "github.com/weedbox/storage-modules/s3_connector"
    "github.com/weedbox/storage-modules/storage_connector"
    "go.uber.org/fx"
    "go.uber.org/zap"
)

func main() {
    app := fx.New(
        fx.Provide(zap.NewDevelopment),

        // Pick a backend — consumers don't change either way:
        s3_connector.Module("storage"),
        // local_storage_connector.Module("storage"),

        fx.Invoke(func(sc storage_connector.StorageConnector) {
            url, _ := sc.WriteAsFile("images/logo.png", imageBytes)
            _ = url
        }),
    )
    app.Run()
}
```

Consumer modules depend on the interface through their `Params`:

```go
type Params struct {
    fx.In
    Storage storage_connector.StorageConnector
}
```

## The shared interface

```go
type StorageConnector interface {
    WriteAsFile(filePath string, content []byte) (string, error)
    SaveFile(req *UploaderReq) (string, error)
    ReadFile(filePath string) ([]byte, error)
    Exists(filePath string) (bool, error)
    DeleteFile(filePath string) error
    DeleteFileWithPrefix(prefix string) error
    PublicURL(filePath string) string
}
```

Backend-specific features that don't generalize stay on the concrete type. For
example, S3 presigned URLs live on `*s3_connector.S3Connector`; reach them by
type-asserting the injected interface:

```go
if s3c, ok := sc.(*s3_connector.S3Connector); ok {
    link, _ := s3c.PresignGetURL("avatars/profile.jpg", 10*time.Minute)
    _ = link
}
```

### Running multiple backends side by side

`Module()` uses `fxmodule.InterfaceModule`, which registers each backend under
`name:"<scope>"` and lets the first one claim the unnamed default. Inject the
named results to use more than one at once:

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

## Configuration

Each module reads [Viper](https://github.com/spf13/viper) config namespaced under the scope passed to `Module()`. See each module's README for the full table.

### s3_connector

```toml
[storage]
bucket_name = "my-bucket"
region = "ap-northeast-1"
access_key_id = "AKIA..."
secret_access_key = "..."
# For MinIO / Cloudflare R2, also set:
# endpoint = "http://localhost:9000"
# use_path_style = true
# public_base_url = "http://localhost:9000/my-bucket"
```

### local_storage_connector

```toml
[storage]
root_dir = "./data/uploads"
base_url = "https://cdn.example.com"   # optional; empty = root-relative "/key"
```

> When the S3 `access_key_id` / `secret_access_key` are empty, the connector falls back to the standard AWS credential chain (env vars, shared config, IAM roles) — the recommended approach in production.

## Public access vs. presigned URLs (S3)

Unlike GCS, modern S3 buckets usually have **Block Public Access** enabled and ACLs disabled, so `s3_connector` does **not** make objects public by default:

- For **public** delivery, set `acl = "public-read"` (only when the bucket allows ACLs) or front the bucket with a CDN / bucket policy and set `public_base_url`.
- For **private, time-limited** access, use `PresignGetURL` / `PresignPutURL`. No public access required.

`local_storage_connector` has no equivalent concern: `PublicURL` simply builds a `base_url`-prefixed or root-relative path; how those paths are served is up to your HTTP layer.

## API Reference

### Module

```go
func Module(scope string) fx.Option
```

Each backend exposes a `Module(scope)` that provides `storage_connector.StorageConnector`. The `scope` namespaces all configuration keys and logger output.

### Interface methods

| Method | Description |
|--------|-------------|
| `WriteAsFile(filePath string, content []byte) (string, error)` | Write raw bytes, return the access URL |
| `SaveFile(req *UploaderReq) (string, error)` | Write base64 content under `{category}/{filename}`, return the access URL |
| `ReadFile(filePath string) ([]byte, error)` | Read an object's full content |
| `Exists(filePath string) (bool, error)` | Whether an object exists |
| `DeleteFile(filePath string) error` | Delete a single object (idempotent) |
| `DeleteFileWithPrefix(prefix string) error` | Delete all objects under a prefix |
| `PublicURL(filePath string) string` | Build the access (unsigned) URL for a key |

### S3-specific methods (`*s3_connector.S3Connector`)

| Method | Description |
|--------|-------------|
| `PresignGetURL(filePath string, expiry time.Duration) (string, error)` | Time-limited signed download URL |
| `PresignPutURL(filePath string, expiry time.Duration) (string, error)` | Time-limited signed upload URL |
| `GetBucketName() string` | Configured bucket name |
| `GetClient() *s3.Client` | Underlying AWS S3 client |
| `GetPresignClient() *s3.PresignClient` | Underlying presign client |

For `PresignGetURL` / `PresignPutURL`, pass `expiry <= 0` to use the configured `presign_expiry` default.

### Usage Examples

```go
// Write raw bytes (any backend)
url, err := sc.WriteAsFile("images/photo.png", imageBytes)

// Write base64 content with an auto-generated UUID filename
url, err = sc.SaveFile(&storage_connector.UploaderReq{
    Category: "avatars",
    FileName: "profile.jpg",       // optional, auto-generates UUID if empty
    RawData:  base64EncodedString, // base64-encoded file content
})

// Read it back / check existence
data, err := sc.ReadFile("images/photo.png")
ok, err := sc.Exists("images/photo.png")

// Delete everything under a prefix
err = sc.DeleteFileWithPrefix("avatars/user-123/")
```

### Types

#### UploaderReq

```go
type UploaderReq struct {
    FileName string `json:"file_name"` // Target filename (optional, auto-generates UUID if empty)
    Category string `json:"category"`  // Category/directory path (object key prefix)
    RawData  string `json:"rowData"`   // Base64-encoded file content
}
```

## Mapping from gcp-modules

| gcp-modules (GCS) | storage-modules |
|-------------------|-----------------|
| `WriteAsFile` | `WriteAsFile` |
| `SaveFile` | `SaveFile` |
| `DeleteFile` | `DeleteFile` |
| `DeleteFileWithPrefix` | `DeleteFileWithPrefix` |
| `GetBucket() *storage.BucketHandle` | `GetBucketName() string` (S3) |
| `GetClient() *storage.Client` | `GetClient() *s3.Client` (S3) |
| (public-read by default) | `PresignGetURL` / `PresignPutURL` / `PublicURL` (S3) |

## License

See [LICENSE](LICENSE) for details.
