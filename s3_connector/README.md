# s3_connector

An S3 (and S3-compatible) object storage connector for [Weedbox](https://github.com/weedbox) applications, built with [Uber Fx](https://github.com/uber-go/fx) and the official [AWS SDK for Go v2](https://github.com/aws/aws-sdk-go-v2).

It mirrors the high-level file API of [`weedbox/gcp-modules`](https://github.com/weedbox/gcp-modules)'s `bucket_connector`, so switching between GCS and S3 backends requires minimal code changes. A configurable endpoint lets the same connector talk to **AWS S3**, **Cloudflare R2**, **MinIO**, or any S3-compatible service.

## Import

```go
import "github.com/weedbox/storage-modules/s3_connector"
```

## Configuration

All keys are namespaced under the scope passed to `Module()` and resolved through [Viper](https://github.com/spf13/viper) (TOML file or environment variables).

| Key | Description | Default |
|-----|-------------|---------|
| `{scope}.bucket_name` | S3 bucket name | `example.com` |
| `{scope}.region` | AWS region | `us-east-1` |
| `{scope}.endpoint` | Custom S3 endpoint (R2 / MinIO / GCS interop). Empty = standard AWS | `""` |
| `{scope}.access_key_id` | Access key. Empty = default AWS credential chain | `""` |
| `{scope}.secret_access_key` | Secret key. Empty = default AWS credential chain | `""` |
| `{scope}.use_path_style` | Path-style addressing (required by most S3-compatible servers) | `false` |
| `{scope}.acl` | Canned ACL applied on upload (e.g. `public-read`). Empty = none | `""` |
| `{scope}.public_base_url` | Base URL used to build public URLs (e.g. a CDN / R2 public domain) | `""` |
| `{scope}.presign_expiry` | Default expiry for presigned URLs | `15m` |

> When `access_key_id` / `secret_access_key` are empty, the connector falls back to the standard AWS credential chain (env vars, shared config, IAM role) — the recommended approach in production.

### TOML — AWS S3

```toml
[s3_storage]
bucket_name = "my-bucket"
region = "ap-northeast-1"
access_key_id = "AKIA..."
secret_access_key = "..."
```

### TOML — MinIO / Cloudflare R2

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

### Environment variables

```bash
export S3_STORAGE_BUCKET_NAME=my-bucket
export S3_STORAGE_REGION=ap-northeast-1
export S3_STORAGE_ACCESS_KEY_ID=AKIA...
export S3_STORAGE_SECRET_ACCESS_KEY=...
```

## Wiring into an Fx app

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
            // bc is ready to use here
        }),
    )
    app.Run()
}
```

Any other module can then depend on `*s3_connector.S3Connector` through its `Params` struct:

```go
type Params struct {
    fx.In
    Storage *s3_connector.S3Connector
}
```

## Usage examples

> Runnable, compile-checked versions of these live in [`example_test.go`](./example_test.go) and render under `go doc`.

### Upload raw bytes — `WriteAsFile`

```go
content := []byte("hello world")

url, err := bc.WriteAsFile("docs/readme.txt", content)
if err != nil {
    return err
}
// url -> https://my-bucket.s3.ap-northeast-1.amazonaws.com/docs/readme.txt
```

The object's `Content-Type` is auto-detected from the file extension.

### Upload base64 content — `SaveFile`

Handy for accepting files straight from a JSON API request. When `FileName` is empty a UUID is generated automatically; the object is stored at `{Category}/{FileName}`.

```go
url, err := bc.SaveFile(&s3_connector.UploaderReq{
    Category: "avatars",
    FileName: "profile.jpg",       // optional; auto-generates a UUID if empty
    RawData:  base64EncodedString, // base64-encoded file content
})
```

### Time-limited download link — `PresignGetURL`

Serve private objects without making the bucket public. Pass `0` to use the configured `presign_expiry` default.

```go
link, err := bc.PresignGetURL("avatars/profile.jpg", 10*time.Minute)
// hand `link` to the browser; it expires after 10 minutes
```

### Direct client upload — `PresignPutURL`

Let end users upload straight to S3 without routing bytes through the backend.

```go
uploadURL, err := bc.PresignPutURL("uploads/user-123/photo.png", 15*time.Minute)
// client:  curl -X PUT --upload-file ./photo.png "<uploadURL>"
```

### Public URL — `PublicURL`

Build the unsigned URL of a key. Whether it is actually reachable depends on the bucket's ACL / policy (or your `public_base_url` / CDN).

```go
url := bc.PublicURL("images/logo.png")
```

### Delete one object — `DeleteFile`

Idempotent: deleting a missing key is **not** an error.

```go
err := bc.DeleteFile("docs/readme.txt")
```

### Delete by prefix — `DeleteFileWithPrefix`

Removes every object under a prefix, batched up to 1000 keys per request.

```go
err := bc.DeleteFileWithPrefix("avatars/user-123/")
```

### Escape hatch — `GetClient` / `GetPresignClient`

For operations not wrapped by this connector, reach the underlying SDK clients directly.

```go
out, err := bc.GetClient().HeadObject(ctx, &s3.HeadObjectInput{
    Bucket: aws.String(bc.GetBucketName()),
    Key:    aws.String("docs/readme.txt"),
})
```

## Public access vs. presigned URLs

Unlike GCS, modern S3 buckets usually have **Block Public Access** enabled and ACLs disabled (Object Ownership = *bucket owner enforced*). This connector therefore does **not** make objects public by default:

- **Public delivery** — set `acl = "public-read"` (only when the bucket allows ACLs), or front the bucket with a CDN / bucket policy and set `public_base_url`. `WriteAsFile` / `SaveFile` / `PublicURL` return the resulting public URL string.
- **Private, time-limited access** — use `PresignGetURL` (download) and `PresignPutURL` (upload). No public access is required.

## Method reference

| Method | Description |
|--------|-------------|
| `GetBucketName() string` | Configured bucket name |
| `GetClient() *s3.Client` | Underlying AWS S3 client |
| `GetPresignClient() *s3.PresignClient` | Underlying presign client |
| `WriteAsFile(filePath string, content []byte) (string, error)` | Upload raw bytes → public URL |
| `SaveFile(req *UploaderReq) (string, error)` | Upload base64 under `{category}/{filename}` → public URL |
| `DeleteFile(filePath string) error` | Delete a single object (idempotent) |
| `DeleteFileWithPrefix(filePath string) error` | Delete all objects under a prefix (batched) |
| `PresignGetURL(filePath string, expiry time.Duration) (string, error)` | Time-limited signed download URL |
| `PresignPutURL(filePath string, expiry time.Duration) (string, error)` | Time-limited signed upload URL |
| `PublicURL(filePath string) string` | Build the public (unsigned) URL for a key |

## Types

### UploaderReq

```go
type UploaderReq struct {
    FileName string `json:"file_name"` // Target filename (optional, auto-generates UUID if empty)
    Category string `json:"category"`  // Category/directory path (object key prefix) in the bucket
    RawData  string `json:"rowData"`   // Base64-encoded file content
}
```

## Mapping from gcp-modules

| gcp-modules (GCS) | s3_connector (S3) |
|-------------------|-------------------|
| `WriteAsFile` | `WriteAsFile` (`PutObject`) |
| `SaveFile` | `SaveFile` (`PutObject`) |
| `DeleteFile` | `DeleteFile` (`DeleteObject`) |
| `DeleteFileWithPrefix` | `DeleteFileWithPrefix` (`ListObjectsV2` + `DeleteObjects`) |
| `GetBucket() *storage.BucketHandle` | `GetBucketName() string` |
| `GetClient() *storage.Client` | `GetClient() *s3.Client` |
| (public-read by default) | `PresignGetURL` / `PresignPutURL` / `PublicURL` |
