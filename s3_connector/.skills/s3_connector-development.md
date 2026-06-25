# S3 Connector Development Skill

## Module Overview

The `s3_connector` module is a connector-style Weedbox module that wraps the
official **AWS SDK for Go v2** to provide high-level object-storage operations
against **AWS S3** or any **S3-compatible** service (Cloudflare R2, MinIO, GCS
interoperability mode).

It intentionally mirrors the file API of `weedbox/gcp-modules`'s
`bucket_connector`, so an application can switch between GCS and S3 backends
with minimal code changes.

**Module Path**: `s3_connector/`

**Provided Type**: `*s3_connector.S3Connector` (via `s3_connector.Module(scope)`)

**Main Features**:
- Upload raw bytes (`WriteAsFile`) and base64 payloads (`SaveFile`)
- Delete single objects and entire prefixes (batched)
- Presigned download / upload URLs (private, time-limited access)
- Public URL construction (AWS virtual-hosted, custom endpoint, CDN base URL)
- S3-compatible via configurable endpoint + path-style addressing
- Fx lifecycle management (client built in `OnStart`)

## Module Type & Wiring

This is a **plain Fx module** (concrete struct), matching the gcp-modules
pattern — not a `weedbox.Module[P]` generic and not (yet) an
`fxmodule.InterfaceModule`. It is injected **without** a `name` tag:

```go
type Params struct {
    fx.In
    Storage *s3_connector.S3Connector
}
```

Registration:

```go
func initModules() ([]fx.Option, error) {
    return []fx.Option{
        fx.Supply(config),
        logger.Module(),
        s3_connector.Module("s3_storage"),
        daemon.Module("daemon"),
    }, nil
}
```

> Future option: if multiple storage backends must coexist in one `fx.App`,
> define a shared storage interface and convert this to Method 3
> (`fxmodule.InterfaceModule`). Currently it exposes a concrete struct.

## Configuration

All keys are namespaced under the `scope` passed to `Module()` and read via
Viper. Defaults are registered in `initDefaultConfigs()`.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `{scope}.bucket_name` | string | `example.com` | S3 bucket name |
| `{scope}.region` | string | `us-east-1` | AWS region |
| `{scope}.endpoint` | string | `""` | Custom endpoint (R2 / MinIO / GCS interop). Empty = standard AWS |
| `{scope}.access_key_id` | string | `""` | Access key. Empty = default AWS credential chain |
| `{scope}.secret_access_key` | string | `""` | Secret key. Empty = default AWS credential chain |
| `{scope}.use_path_style` | bool | `false` | Path-style addressing (required by most S3-compatible servers) |
| `{scope}.acl` | string | `""` | Canned ACL applied on upload (e.g. `public-read`). Empty = none |
| `{scope}.public_base_url` | string | `""` | Base URL for building public URLs (CDN / R2 public domain) |
| `{scope}.presign_expiry` | duration | `15m` | Default expiry for presigned URLs |

Constants in `connector.go`:

```go
const (
    DefaultBucketName    = "example.com"
    DefaultRegion        = "us-east-1"
    DefaultPresignExpiry = 15 * time.Minute
)
```

### Credential resolution

`onStart` uses static credentials only when **both** `access_key_id` and
`secret_access_key` are non-empty; otherwise it falls back to the default AWS
credential chain (`config.LoadDefaultConfig`) — env vars, shared config files,
IAM roles. Production deployments should prefer the chain.

### Endpoint / path-style

A non-empty `endpoint` sets `s3.Options.BaseEndpoint`; `use_path_style` sets
`s3.Options.UsePathStyle`. Most S3-compatible servers (MinIO) require
`use_path_style = true`.

## Lifecycle

| Hook | Behavior |
|------|----------|
| `onStart(ctx)` | Builds `aws.Config`, creates `*s3.Client` and `*s3.PresignClient`, logs config |
| `onStop(ctx)`  | Logs only — the S3 client holds no connection that needs closing |

## Public Structures

### S3Connector

```go
type S3Connector struct {
    params        Params
    logger        *zap.Logger
    client        *s3.Client
    presignClient *s3.PresignClient
    scope         string
}
```

### UploaderReq

```go
type UploaderReq struct {
    FileName string `json:"file_name"` // Target filename (optional, auto-generates UUID if empty)
    Category string `json:"category"`  // Object key prefix (directory) in the bucket
    RawData  string `json:"rowData"`   // Base64-encoded file content
}
```

> Note the JSON tag `rowData` (not `rawData`) — kept identical to gcp-modules
> for drop-in request compatibility.

## Methods

### GetBucketName

```go
func (c *S3Connector) GetBucketName() string
```
Returns the configured bucket name. (Replaces gcp-modules' `GetBucket()
*storage.BucketHandle`, which has no S3 equivalent.)

### GetClient / GetPresignClient

```go
func (c *S3Connector) GetClient() *s3.Client
func (c *S3Connector) GetPresignClient() *s3.PresignClient
```
Escape hatches to the underlying SDK clients for operations not wrapped here.

### WriteAsFile

```go
func (c *S3Connector) WriteAsFile(filePath string, content []byte) (string, error)
```
- Uploads raw bytes to `filePath` via `PutObject`.
- `Content-Type` auto-detected from the file extension (`mime.TypeByExtension`,
  fallback `application/octet-stream`).
- Applies a canned ACL only if `{scope}.acl` is set.
- Returns the public URL (see `PublicURL`).

### SaveFile

```go
func (c *S3Connector) SaveFile(req *UploaderReq) (string, error)
```
- Base64-decodes `req.RawData`.
- Object key = `{req.Category}/{req.FileName}`; if `FileName` is empty a UUID v4
  (`github.com/google/uuid`) is generated.
- Same upload behavior as `WriteAsFile`. Returns the public URL.

### DeleteFile

```go
func (c *S3Connector) DeleteFile(filePath string) error
```
- `DeleteObject`. **Idempotent** — S3 returns success for a missing key, so a
  non-existent object is not an error.

### DeleteFileWithPrefix

```go
func (c *S3Connector) DeleteFileWithPrefix(filePath string) error
```
- Lists objects with `ListObjectsV2` (paginated) and deletes them with
  `DeleteObjects` in batches of up to 1000 keys (`Quiet: true`).

### PresignGetURL / PresignPutURL

```go
func (c *S3Connector) PresignGetURL(filePath string, expiry time.Duration) (string, error)
func (c *S3Connector) PresignPutURL(filePath string, expiry time.Duration) (string, error)
```
- Generate time-limited signed URLs for download / direct client upload.
- `expiry <= 0` falls back to `{scope}.presign_expiry`.

### PublicURL

```go
func (c *S3Connector) PublicURL(filePath string) string
```
Builds the unsigned URL of a key. Resolution order:
1. `{scope}.public_base_url` set → `{public_base_url}/{key}`.
2. `endpoint` set + `use_path_style` → `{endpoint}/{bucket}/{key}`.
3. `endpoint` set + virtual-hosted → `{scheme}://{bucket}.{endpoint-host}/{key}`.
4. Standard AWS → `https://{bucket}.s3.{region}.amazonaws.com/{key}`.

> Reachability depends on the bucket's ACL / policy (or CDN). This is the main
> behavioral difference from GCS — see "Public access vs presigned" below.

## Public access vs presigned (important)

Modern S3 buckets typically have **Block Public Access** enabled and ACLs
disabled (Object Ownership = bucket owner enforced), so the GCS habit of
"upload public-read + return a public URL" does **not** transfer 1:1.

- For **public** delivery: set `acl = "public-read"` (only if the bucket allows
  ACLs) **or** front the bucket with a CDN / bucket policy and set
  `public_base_url`.
- For **private** delivery: use `PresignGetURL` / `PresignPutURL` — no public
  access needed. This is the recommended default.

## Error Handling

The connector returns SDK errors directly (wrapped with structured `zap` logs);
it defines no sentinel error variables. `DeleteFile` deliberately swallows the
"key not found" case to stay idempotent.

## Usage Examples

Compile-checked versions live in `example_test.go` (godoc examples).

### Upload bytes

```go
url, err := bc.WriteAsFile("images/logo.png", imageBytes)
```

### Upload base64 (e.g. from a JSON API)

```go
url, err := bc.SaveFile(&s3_connector.UploaderReq{
    Category: "avatars",
    FileName: "profile.jpg",       // optional, UUID if empty
    RawData:  base64EncodedString,
})
```

### Private, time-limited links

```go
download, err := bc.PresignGetURL("avatars/profile.jpg", 10*time.Minute)
upload,   err := bc.PresignPutURL("uploads/u-123/p.png", 15*time.Minute)
```

### Delete

```go
err := bc.DeleteFile("images/logo.png")              // single, idempotent
err  = bc.DeleteFileWithPrefix("avatars/user-123/")  // whole prefix, batched
```

## Relationships with Other Modules

- **Depends on**: `*zap.Logger` (from `common-modules/logger`), Viper config,
  `fx.Lifecycle`.
- **Mirrors**: `weedbox/gcp-modules` `bucket_connector` (GCS) — same method
  shapes and `UploaderReq` contract.
- **Depended on by**: any application module needing object storage; inject
  `*s3_connector.S3Connector` via `Params`.

## Dependencies (go.mod)

| Package | Purpose |
|---------|---------|
| `github.com/aws/aws-sdk-go-v2/config` | Load AWS config / credential chain |
| `github.com/aws/aws-sdk-go-v2/credentials` | Static credentials provider |
| `github.com/aws/aws-sdk-go-v2/service/s3` | S3 client, presign client, paginator |
| `github.com/aws/aws-sdk-go-v2/service/s3/types` | S3 input/types (ACL, ObjectIdentifier, Delete) |
| `github.com/google/uuid` | Auto-generated filenames |
| `github.com/spf13/viper` | Configuration |
| `go.uber.org/fx` / `go.uber.org/zap` | DI and logging |

## Verification Checklist

- [x] Module overview explains purpose
- [x] Configuration keys and defaults documented
- [x] All public methods documented with behaviors
- [x] Lifecycle hooks documented
- [x] Error / idempotency behavior explained
- [x] Usage examples are copy-paste ready
- [x] Relationship with gcp-modules and consumers explained
