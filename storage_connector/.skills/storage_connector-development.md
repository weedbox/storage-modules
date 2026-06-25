# Storage Connector (shared contract) Development Skill

## Module Overview

`storage_connector` is **not** a wired Fx module — it is the shared contract
package for `weedbox/storage-modules`. It declares the backend-agnostic
`StorageConnector` interface, the `UploaderReq` request type, and the
`DetectContentType` helper. Every concrete backend (`s3_connector`,
`local_storage_connector`) implements this interface and registers itself as the
provider of `storage_connector.StorageConnector`.

**Module Path**: `storage_connector/`

**Provided Type**: none directly — backends provide
`storage_connector.StorageConnector` via their own `Module(scope)`.

**Purpose**:
- Give applications one stable interface to depend on, decoupled from any backend
- Let backends be swapped (S3 ↔ local filesystem) with zero consumer changes
- Hold cross-backend helpers (`DetectContentType`) and the shared `UploaderReq`

## The Interface

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

### Design rule: keep the interface backend-neutral

Only methods every backend can implement belong here. Backend-specific
capabilities stay on the concrete type and are reached via type assertion:

- `*s3_connector.S3Connector` adds `PresignGetURL`, `PresignPutURL`, `GetClient`,
  `GetPresignClient`, `GetBucketName`.
- `*local_storage_connector.LocalStorageConnector` adds `GetRootDir`.

```go
if s3c, ok := sc.(*s3_connector.S3Connector); ok {
    link, _ := s3c.PresignGetURL(key, 10*time.Minute)
}
```

## UploaderReq

```go
type UploaderReq struct {
    FileName string `json:"file_name"` // optional; UUID generated if empty
    Category string `json:"category"`  // key prefix (directory)
    RawData  string `json:"rowData"`   // base64-encoded content
}
```

> JSON tag `rowData` (not `rawData`) is intentional — kept identical to
> `weedbox/gcp-modules` for drop-in request compatibility. Do not "fix" it.

Each backend declares `type UploaderReq = storage_connector.UploaderReq` (a type
alias) so callers can still write `s3_connector.UploaderReq` /
`local_storage_connector.UploaderReq`.

## DetectContentType

```go
func DetectContentType(filePath string) string
```

Maps a file extension to a MIME type via `mime.TypeByExtension`, falling back to
`application/octet-stream`. Used by object-store backends (S3) to set
`Content-Type` on upload. Filesystem backends ignore it.

## Module Type & Wiring (for backends)

Backends register with **Method 3** —
`fxmodule.InterfaceModule[storage_connector.StorageConnector]` from
`github.com/weedbox/weedbox/fxmodule`:

```go
func Module(scope string) fx.Option {
    return fxmodule.InterfaceModule[storage_connector.StorageConnector](
        scope,
        func(p Params) storage_connector.StorageConnector {
            c := &XxxConnector{ /* ... */ }
            c.initDefaultConfigs()
            p.Lifecycle.Append(fx.Hook{OnStart: c.onStart, OnStop: c.onStop})
            return c
        },
    )
}
```

`InterfaceModule` behavior:
- Registers the ctor as `name:"<scope>"` returning the interface.
- The **first** backend loaded also claims the unnamed default (via an Alias), so
  a single-backend app can inject `storage_connector.StorageConnector` with no
  name tag.
- Multiple backends coexist; consumers disambiguate with `name:"<scope>"`.

### Consuming

Single backend:

```go
type Params struct {
    fx.In
    Storage storage_connector.StorageConnector
}
```

Multiple backends:

```go
fx.Invoke(func(in struct {
    fx.In
    Remote storage_connector.StorageConnector `name:"s3"`
    Local  storage_connector.StorageConnector `name:"local"`
}) { /* ... */ })
```

## Testing Notes

- `DetectContentType` is unit-tested in `storage_connector_test.go`.
- When an Fx test builds more than one app loading the same backend, call
  `fxmodule.ResetClaim[storage_connector.StorageConnector]()` between apps so the
  unnamed-default claim is released.

## Adding a New Backend (checklist)

1. New package `xxx_connector/`.
2. `type UploaderReq = storage_connector.UploaderReq`.
3. Implement every `StorageConnector` method.
4. `var _ storage_connector.StorageConnector = (*XxxConnector)(nil)` compile check.
5. `Module(scope)` via `fxmodule.InterfaceModule`.
6. `initDefaultConfigs()` with namespaced Viper keys + lifecycle hooks.
7. README, `.skills/` doc, and `example_test.go`.

## Relationships with Other Modules

- **Implemented by**: `s3_connector`, `local_storage_connector`.
- **Depended on by**: any application module needing storage — inject
  `storage_connector.StorageConnector`, not a concrete type.
- **Mirrors**: the file API of `weedbox/gcp-modules` `bucket_connector`.

## Dependencies (go.mod)

| Package | Purpose |
|---------|---------|
| `mime`, `path/filepath` (stdlib) | `DetectContentType` |

The contract package itself pulls in no third-party dependencies.

## Verification Checklist

- [x] Interface methods documented
- [x] Backend-neutrality design rule explained
- [x] UploaderReq + `rowData` tag rationale documented
- [x] InterfaceModule wiring (single + multi backend) explained
- [x] New-backend checklist provided
