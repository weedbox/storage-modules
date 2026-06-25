# local_storage_connector

A local-filesystem implementation of [`storage_connector.StorageConnector`](../storage_connector) for [Weedbox](https://github.com/weedbox) applications, built with [Uber Fx](https://github.com/uber-go/fx).

It stores objects as files under a configured root directory, making it a drop-in alternative to [`s3_connector`](../s3_connector) — ideal for local development, tests, and single-node deployments where an object store would be overkill.

## Import

```go
import "github.com/weedbox/storage-modules/local_storage_connector"
```

## Configuration

All keys are namespaced under the scope passed to `Module()` and resolved through [Viper](https://github.com/spf13/viper) (TOML file or environment variables).

| Key | Description | Default |
|-----|-------------|---------|
| `{scope}.root_dir` | Directory that holds all stored files. Created on start. | `./storage` |
| `{scope}.base_url` | Base URL prefix for `PublicURL`. Empty = return root-relative `/key`. | `""` |

### TOML

```toml
[local_storage]
root_dir = "./data/uploads"
base_url = "https://cdn.example.com"
```

### Environment variables

```bash
export LOCAL_STORAGE_ROOT_DIR=./data/uploads
export LOCAL_STORAGE_BASE_URL=https://cdn.example.com
```

## Wiring into an Fx app

`Module` provides the shared `storage_connector.StorageConnector`, so consumers depend on the interface and never reference the concrete type:

```go
package main

import (
    "github.com/weedbox/storage-modules/local_storage_connector"
    "github.com/weedbox/storage-modules/storage_connector"
    "go.uber.org/fx"
    "go.uber.org/zap"
)

func main() {
    app := fx.New(
        fx.Provide(zap.NewDevelopment),
        local_storage_connector.Module("local_storage"),
        fx.Invoke(func(sc storage_connector.StorageConnector) {
            // sc is ready to use here
        }),
    )
    app.Run()
}
```

Any other module can then depend on the interface through its `Params` struct:

```go
type Params struct {
    fx.In
    Storage storage_connector.StorageConnector
}
```

## Usage examples

> Compile-checked versions live in [`example_test.go`](./example_test.go) and render under `go doc`.

### Write raw bytes — `WriteAsFile`

```go
url, err := sc.WriteAsFile("docs/readme.txt", []byte("hello world"))
// writes {root_dir}/docs/readme.txt; url -> "/docs/readme.txt" (or "{base_url}/docs/readme.txt")
```

Parent directories are created automatically. The content type is irrelevant on disk, so nothing is detected.

### Write base64 content — `SaveFile`

Handy for accepting files straight from a JSON API request. When `FileName` is empty a UUID is generated; the file is stored at `{Category}/{FileName}`.

```go
url, err := sc.SaveFile(&storage_connector.UploaderReq{
    Category: "avatars",
    FileName: "profile.jpg",       // optional; auto-generates a UUID if empty
    RawData:  base64EncodedString, // base64-encoded file content
})
```

### Read content back — `ReadFile`

```go
data, err := sc.ReadFile("docs/readme.txt")
```

### Check existence — `Exists`

```go
ok, err := sc.Exists("docs/readme.txt")
```

### Delete one file — `DeleteFile`

Idempotent: deleting a missing file is **not** an error.

```go
err := sc.DeleteFile("docs/readme.txt")
```

### Delete by prefix — `DeleteFileWithPrefix`

Removes every file whose key starts with the prefix.

```go
err := sc.DeleteFileWithPrefix("avatars/user-123/")
```

### Public URL — `PublicURL`

```go
url := sc.PublicURL("images/logo.png")
// base_url set -> "https://cdn.example.com/images/logo.png"
// base_url empty -> "/images/logo.png"
```

## Path safety

Every key is resolved against `root_dir` before any filesystem access. Legitimate internal normalization (`a/../b.txt` → `b.txt`) is preserved, but a key whose cleaned path escapes the root — such as `../../etc/passwd` — is rejected with `ErrInvalidPath`, so it can never read or write outside the storage directory.

This is a lexical check (it does not resolve symlinks); keep `root_dir` owned by the application.

## Method reference

| Method | Description |
|--------|-------------|
| `GetRootDir() string` | Configured root directory |
| `WriteAsFile(filePath string, content []byte) (string, error)` | Write raw bytes → access URL |
| `SaveFile(req *UploaderReq) (string, error)` | Write base64 under `{category}/{filename}` → access URL |
| `ReadFile(filePath string) ([]byte, error)` | Read full file content |
| `Exists(filePath string) (bool, error)` | Whether a file exists |
| `DeleteFile(filePath string) error` | Delete a single file (idempotent) |
| `DeleteFileWithPrefix(prefix string) error` | Delete all files under a prefix |
| `PublicURL(filePath string) string` | Build the access URL for a key |

## Types

### UploaderReq

```go
type UploaderReq struct {
    FileName string `json:"file_name"` // Target filename (optional, auto-generates UUID if empty)
    Category string `json:"category"`  // Category/directory path (key prefix)
    RawData  string `json:"rowData"`   // Base64-encoded file content
}
```

`UploaderReq` is an alias for [`storage_connector.UploaderReq`](../storage_connector).
