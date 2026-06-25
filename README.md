# storage-modules

S3-compatible object storage modules for [Weedbox](https://github.com/weedbox) applications, built with [Uber Fx](https://github.com/uber-go/fx) dependency injection.

The API mirrors [`weedbox/gcp-modules`](https://github.com/weedbox/gcp-modules) so the two backends are largely interchangeable. The underlying SDK is the official [AWS SDK for Go v2](https://github.com/aws/aws-sdk-go-v2), and a configurable endpoint lets the same connector talk to AWS S3, Cloudflare R2, MinIO, or any S3-compatible service.

## Modules

### s3_connector

An S3 object storage module that provides bucket operations including file upload, deletion, presigned URLs, and public URL generation with automatic lifecycle management.

## Installation

```bash
go get github.com/weedbox/storage-modules
```

## Quick Start

```go
package main

import (
    "github.com/weedbox/storage-modules/s3_connector"
    "go.uber.org/fx"
    "go.uber.org/zap"
)

func main() {
    app := fx.New(
        fx.Provide(zap.NewDevelopment),
        s3_connector.Module("s3_storage"),
        fx.Invoke(func(bc *s3_connector.S3Connector) {
            // Use the connector
        }),
    )
    app.Run()
}
```

## Configuration

The module uses [Viper](https://github.com/spf13/viper) for configuration. All config keys are namespaced under the scope name passed to `Module()`.

| Key | Description | Default |
|-----|-------------|---------|
| `{scope}.bucket_name` | S3 bucket name | `example.com` |
| `{scope}.region` | AWS region | `us-east-1` |
| `{scope}.endpoint` | Custom S3 endpoint (R2 / MinIO / GCS interop). Empty = standard AWS | `""` |
| `{scope}.access_key_id` | Access key. Empty = default AWS credential chain | `""` |
| `{scope}.secret_access_key` | Secret key. Empty = default AWS credential chain | `""` |
| `{scope}.use_path_style` | Use path-style addressing (required by most S3-compatible servers) | `false` |
| `{scope}.acl` | Canned ACL applied on upload (e.g. `public-read`). Empty = none | `""` |
| `{scope}.public_base_url` | Base URL used to build public URLs (e.g. a CDN / R2 public domain) | `""` |
| `{scope}.presign_expiry` | Default expiry for presigned URLs | `15m` |

### Example (AWS S3)

```toml
[s3_storage]
bucket_name = "my-bucket"
region = "ap-northeast-1"
access_key_id = "AKIA..."
secret_access_key = "..."
```

### Example (MinIO / Cloudflare R2)

```toml
[s3_storage]
bucket_name = "my-bucket"
region = "auto"
endpoint = "http://localhost:9000"
access_key_id = "minioadmin"
secret_access_key = "minioadmin"
use_path_style = true
public_base_url = "http://localhost:9000/my-bucket"
```

Or via environment variables (with Viper's automatic env binding):

```bash
export S3_STORAGE_BUCKET_NAME=my-bucket
export S3_STORAGE_REGION=ap-northeast-1
export S3_STORAGE_ACCESS_KEY_ID=AKIA...
export S3_STORAGE_SECRET_ACCESS_KEY=...
```

> **Note on credentials:** when `access_key_id` / `secret_access_key` are left empty, the connector falls back to the standard AWS credential chain (environment variables, shared config files, and IAM roles), which is the recommended approach in production.

## Public access vs. presigned URLs

Unlike GCS, modern S3 buckets usually have **Block Public Access** enabled and ACLs disabled (Object Ownership = *bucket owner enforced*). This module therefore does **not** make objects public by default:

- For **public** delivery, either set `acl = "public-read"` (only works when the bucket allows ACLs) or front the bucket with a CDN / bucket policy and set `public_base_url`. `WriteAsFile` / `SaveFile` / `PublicURL` return the resulting public URL string.
- For **private, time-limited** access, use `PresignGetURL` (download) and `PresignPutURL` (direct client upload). No public access is required.

## API Reference

### Module

```go
func Module(scope string) fx.Option
```

Creates an Fx module that provides a `*S3Connector`. The `scope` parameter namespaces all configuration keys and logger output.

### S3Connector Methods

| Method | Description |
|--------|-------------|
| `GetBucketName() string` | Returns the configured bucket name |
| `GetClient() *s3.Client` | Returns the underlying AWS S3 client |
| `GetPresignClient() *s3.PresignClient` | Returns the underlying presign client |
| `WriteAsFile(filePath string, content []byte) (string, error)` | Uploads raw bytes, returns the public URL |
| `SaveFile(req *UploaderReq) (string, error)` | Uploads base64 content under `{category}/{filename}`, returns the public URL |
| `DeleteFile(filePath string) error` | Deletes a single object (idempotent) |
| `DeleteFileWithPrefix(filePath string) error` | Deletes all objects under a prefix (batched) |
| `PresignGetURL(filePath string, expiry time.Duration) (string, error)` | Time-limited signed download URL |
| `PresignPutURL(filePath string, expiry time.Duration) (string, error)` | Time-limited signed upload URL |
| `PublicURL(filePath string) string` | Builds the public (unsigned) URL for a key |

For `PresignGetURL` / `PresignPutURL`, pass `expiry <= 0` to use the configured `presign_expiry` default.

### Usage Examples

```go
// Upload raw bytes
url, err := bc.WriteAsFile("images/photo.png", imageBytes)

// Upload base64 content with an auto-generated UUID filename
url, err := bc.SaveFile(&s3_connector.UploaderReq{
    Category: "avatars",
    FileName: "profile.jpg",       // optional, auto-generates UUID if empty
    RawData:  base64EncodedString, // base64-encoded file content
})

// Generate a 10-minute download link
link, err := bc.PresignGetURL("avatars/profile.jpg", 10*time.Minute)

// Delete everything under a prefix
err := bc.DeleteFileWithPrefix("avatars/user-123/")
```

### Types

#### UploaderReq

```go
type UploaderReq struct {
    FileName string `json:"file_name"` // Target filename (optional, auto-generates UUID if empty)
    Category string `json:"category"`  // Category/directory path (object key prefix) in the bucket
    RawData  string `json:"rowData"`   // Base64-encoded file content
}
```

## Mapping from gcp-modules

| gcp-modules (GCS) | storage-modules (S3) |
|-------------------|----------------------|
| `WriteAsFile` | `WriteAsFile` (`PutObject`) |
| `SaveFile` | `SaveFile` (`PutObject`) |
| `DeleteFile` | `DeleteFile` (`DeleteObject`) |
| `DeleteFileWithPrefix` | `DeleteFileWithPrefix` (`ListObjectsV2` + `DeleteObjects`) |
| `GetBucket() *storage.BucketHandle` | `GetBucketName() string` |
| `GetClient() *storage.Client` | `GetClient() *s3.Client` |
| (public-read by default) | `PresignGetURL` / `PresignPutURL` / `PublicURL` |

## License

See [LICENSE](LICENSE) for details.