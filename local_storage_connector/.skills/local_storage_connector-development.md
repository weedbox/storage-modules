# Local Storage Connector Development Skill

## Module Overview

The `local_storage_connector` module stores objects as files on the local
filesystem under a configured root directory. It implements the shared
`storage_connector.StorageConnector` interface, making it a drop-in alternative
to `s3_connector` — ideal for local development, tests, and single-node
deployments.

**Module Path**: `local_storage_connector/`

**Provided Type**: `storage_connector.StorageConnector` (concrete
`*local_storage_connector.LocalStorageConnector`), via
`local_storage_connector.Module(scope)`.

**Main Features**:
- Write raw bytes (`WriteAsFile`) and base64 payloads (`SaveFile`)
- Read (`ReadFile`) and existence checks (`Exists`)
- Delete single files and entire prefixes
- Public URL construction (root-relative or `base_url`-prefixed)
- Path-traversal protection (keys clamped inside the root)
- Fx lifecycle management (root dir created in `OnStart`)

## Module Type & Wiring

This is a **Method 3 connector** registered with
`fxmodule.InterfaceModule[storage_connector.StorageConnector]`. It provides the
shared interface, so consumers inject `storage_connector.StorageConnector` (not
the concrete type):

```go
type Params struct {
    fx.In
    Storage storage_connector.StorageConnector
}
```

Registration:

```go
func initModules() ([]fx.Option, error) {
    return []fx.Option{
        fx.Supply(config),
        logger.Module(),
        local_storage_connector.Module("local_storage"),
        daemon.Module("daemon"),
    }, nil
}
```

Because it shares the `storage_connector.StorageConnector` interface with
`s3_connector`, swapping backends is a one-line change in the wiring; consumers
are untouched. To load both at once, see the multi-backend section in
`storage_connector/.skills/storage_connector-development.md`.

## Configuration

All keys are namespaced under the `scope` passed to `Module()` and read via
Viper. Defaults are registered in `initDefaultConfigs()`.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `{scope}.root_dir` | string | `./storage` | Directory holding all files; created on start |
| `{scope}.base_url` | string | `""` | Prefix for `PublicURL`. Empty = root-relative `/key` |

Constants in `connector.go`:

```go
const DefaultRootDir = "./storage"

var ErrInvalidPath = errors.New("local_storage_connector: invalid path")
```

## Lifecycle

| Hook | Behavior |
|------|----------|
| `onStart(ctx)` | `os.MkdirAll(root, 0o755)`; logs root_dir / base_url |
| `onStop(ctx)`  | Logs only — no resources to release |

## Public Structures

### LocalStorageConnector

```go
type LocalStorageConnector struct {
    params Params
    logger *zap.Logger
    scope  string
}
```

### UploaderReq

```go
type UploaderReq = storage_connector.UploaderReq
```

Alias of the shared type; see `storage_connector` skill for the `rowData` tag
rationale.

## Path Safety (resolve)

`resolve(filePath)` is the security boundary for every filesystem operation:

```go
absRoot, _ := filepath.Abs(c.rootDir())
full := filepath.Join(absRoot, filepath.FromSlash(filePath)) // Join cleans the result
if full != absRoot && !strings.HasPrefix(full, absRoot+string(filepath.Separator)) {
    return "", ErrInvalidPath
}
```

`filepath.Join` cleans the joined path, so legitimate internal normalization
(`a/../b.txt` → `b.txt`) is preserved, while any key whose cleaned result escapes
the root (e.g. `../../etc/passwd`) is rejected with `ErrInvalidPath`. All
read/write/delete/exists paths flow through `resolve`.

> Note: this is a **lexical** containment check — it does not call
> `filepath.EvalSymlinks`. The connector never creates symlinks itself, but if a
> symlink pointing outside the root were planted in the storage directory by
> another process, writes through it could follow it. Keep `root_dir` owned by
> the application.

## Methods

### GetRootDir

```go
func (c *LocalStorageConnector) GetRootDir() string
```
Returns the configured root directory (backend-specific; not on the interface).

### WriteAsFile

```go
func (c *LocalStorageConnector) WriteAsFile(filePath string, content []byte) (string, error)
```
- `MkdirAll` the parent dir, then `os.WriteFile(full, content, 0o644)`.
- Returns `PublicURL(filePath)`.

### SaveFile

```go
func (c *LocalStorageConnector) SaveFile(req *UploaderReq) (string, error)
```
- Base64-decodes `req.RawData`.
- Key = `{req.Category}/{req.FileName}`; UUID v4 filename when `FileName` empty.
- Delegates to `WriteAsFile`.

### ReadFile / Exists

```go
func (c *LocalStorageConnector) ReadFile(filePath string) ([]byte, error)
func (c *LocalStorageConnector) Exists(filePath string) (bool, error)
```
- `ReadFile` → `os.ReadFile`.
- `Exists` → `os.Stat`; `os.IsNotExist` maps to `(false, nil)`.

### DeleteFile

```go
func (c *LocalStorageConnector) DeleteFile(filePath string) error
```
- `os.Remove`; **idempotent** — `os.IsNotExist` is swallowed.

### DeleteFileWithPrefix

```go
func (c *LocalStorageConnector) DeleteFileWithPrefix(prefix string) error
```
- `filepath.WalkDir` over the root, collecting files whose root-relative,
  slash-normalized path `HasPrefix` the cleaned prefix, then removes them.
- Returns `nil` if the root directory does not yet exist.

### PublicURL

```go
func (c *LocalStorageConnector) PublicURL(filePath string) string
```
- `base_url` set → `{base_url}/{key}` (trailing slash trimmed).
- Otherwise → `/{key}` (root-relative).

## Error Handling

Returns stdlib `os` errors directly (wrapped with `zap` logs on write/decode
failures). `resolve` returns `ErrInvalidPath` on traversal. `DeleteFile` and
`DeleteFileWithPrefix` treat missing targets as success.

## Usage Examples

Compile-checked versions live in `example_test.go` (godoc examples).

```go
url, err := sc.WriteAsFile("images/logo.png", imageBytes)

url, err = sc.SaveFile(&local_storage_connector.UploaderReq{
    Category: "avatars",
    FileName: "profile.jpg",       // optional, UUID if empty
    RawData:  base64EncodedString,
})

data, err := sc.ReadFile("images/logo.png")
ok, err := sc.Exists("images/logo.png")
err = sc.DeleteFile("images/logo.png")              // idempotent
err = sc.DeleteFileWithPrefix("avatars/user-123/")  // whole prefix
```

## Testing Notes

- Tests use `t.TempDir()` for `root_dir`, `zap.NewNop()` for the logger, and
  `viper.Reset()` + `viper.Set("{scope}.root_dir", dir)` for config.
- `connector_test.go` covers write/read/exists/delete (incl. idempotency),
  base64 + UUID `SaveFile`, prefix deletion, `PublicURL`, and traversal clamping.

## Relationships with Other Modules

- **Implements**: `storage_connector.StorageConnector`.
- **Depends on**: `*zap.Logger`, Viper config, `fx.Lifecycle`,
  `github.com/google/uuid`.
- **Sibling**: `s3_connector` (same interface, swappable).

## Dependencies (go.mod)

| Package | Purpose |
|---------|---------|
| `github.com/google/uuid` | Auto-generated filenames |
| `github.com/spf13/viper` | Configuration |
| `github.com/weedbox/weedbox/fxmodule` | `InterfaceModule` registration |
| `go.uber.org/fx` / `go.uber.org/zap` | DI and logging |
| `os`, `io/fs`, `path/filepath` (stdlib) | Filesystem operations |

## Verification Checklist

- [x] Module overview explains purpose
- [x] Configuration keys and defaults documented
- [x] All public methods documented with behaviors
- [x] Lifecycle hooks documented
- [x] Path-traversal protection explained
- [x] Error / idempotency behavior explained
- [x] Usage examples are copy-paste ready
- [x] Relationship with storage_connector and s3_connector explained
