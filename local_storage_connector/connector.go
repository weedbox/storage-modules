package local_storage_connector

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/viper"
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/weedbox/storage-modules/storage_connector"
	"github.com/weedbox/weedbox/fxmodule"
)

const (
	DefaultRootDir = "./storage"
)

// ErrInvalidPath is returned when a requested path escapes the configured root
// directory (e.g. via "..").
var ErrInvalidPath = errors.New("local_storage_connector: invalid path")

// UploaderReq is an alias for the shared storage_connector.UploaderReq.
type UploaderReq = storage_connector.UploaderReq

// LocalStorageConnector stores objects on the local filesystem under a
// configured root directory. It implements storage_connector.StorageConnector,
// making it a drop-in alternative to s3_connector (handy for development and
// tests).
type LocalStorageConnector struct {
	params Params
	logger *zap.Logger
	scope  string
}

// Compile-time check that LocalStorageConnector satisfies the shared interface.
var _ storage_connector.StorageConnector = (*LocalStorageConnector)(nil)

type Params struct {
	fx.In

	Lifecycle fx.Lifecycle
	Logger    *zap.Logger
}

// Module registers the local filesystem backend as an implementation of
// storage_connector.StorageConnector. It can be loaded on its own (inject the
// interface without a name tag) or side by side with other backends (inject
// with name:"<scope>").
func Module(scope string) fx.Option {
	return fxmodule.InterfaceModule[storage_connector.StorageConnector](
		scope,
		func(p Params) storage_connector.StorageConnector {
			c := &LocalStorageConnector{
				params: p,
				logger: p.Logger.Named(scope),
				scope:  scope,
			}

			c.initDefaultConfigs()

			p.Lifecycle.Append(fx.Hook{
				OnStart: c.onStart,
				OnStop:  c.onStop,
			})

			return c
		},
	)
}

func (c *LocalStorageConnector) getConfigPath(key string) string {
	return fmt.Sprintf("%s.%s", c.scope, key)
}

func (c *LocalStorageConnector) initDefaultConfigs() {
	viper.SetDefault(c.getConfigPath("root_dir"), DefaultRootDir)
	viper.SetDefault(c.getConfigPath("base_url"), "")
}

func (c *LocalStorageConnector) onStart(ctx context.Context) error {

	root := c.rootDir()

	c.logger.Info("Starting LocalStorageConnector",
		zap.String("root_dir", root),
		zap.String("base_url", viper.GetString(c.getConfigPath("base_url"))),
	)

	return os.MkdirAll(root, 0o755)
}

func (c *LocalStorageConnector) onStop(ctx context.Context) error {
	c.logger.Info("Stopped LocalStorageConnector")
	return nil
}

// GetRootDir returns the configured root directory.
func (c *LocalStorageConnector) GetRootDir() string {
	return c.rootDir()
}

func (c *LocalStorageConnector) rootDir() string {
	return viper.GetString(c.getConfigPath("root_dir"))
}

// resolve maps a storage key to an absolute filesystem path, guaranteeing the
// result stays within the root directory.
//
// filepath.Join cleans the joined path, collapsing any "." / ".." segments, so
// a key like "a/../b.txt" normalizes to "b.txt" within the root. If the cleaned
// result escapes the root directory (e.g. "../../etc/passwd"), ErrInvalidPath is
// returned rather than silently clamping the path.
func (c *LocalStorageConnector) resolve(filePath string) (string, error) {

	absRoot, err := filepath.Abs(c.rootDir())
	if err != nil {
		return "", err
	}

	full := filepath.Join(absRoot, filepath.FromSlash(filePath))

	if full != absRoot && !strings.HasPrefix(full, absRoot+string(filepath.Separator)) {
		return "", ErrInvalidPath
	}

	return full, nil
}

// WriteAsFile writes raw bytes to filePath and returns its access URL.
func (c *LocalStorageConnector) WriteAsFile(filePath string, content []byte) (string, error) {

	full, err := c.resolve(filePath)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}

	if err := os.WriteFile(full, content, 0o644); err != nil {
		c.logger.Error("WriteFile Error", zap.String("path", filePath), zap.Error(err))
		return "", err
	}

	return c.PublicURL(filePath), nil
}

// SaveFile decodes base64 content and stores it under {category}/{filename}.
// If FileName is empty, a UUID is generated. Returns the access URL.
func (c *LocalStorageConnector) SaveFile(req *UploaderReq) (string, error) {

	data, err := base64.StdEncoding.DecodeString(req.RawData)
	if err != nil {
		c.logger.Error("base64 decode Error", zap.Error(err))
		return "", err
	}

	fileName := uuid.New().String()
	if req.FileName != "" {
		fileName = req.FileName
	}

	filePath := fmt.Sprintf("%s/%s", req.Category, fileName)

	return c.WriteAsFile(filePath, data)
}

// ReadFile returns the full content of the object at filePath.
func (c *LocalStorageConnector) ReadFile(filePath string) ([]byte, error) {

	full, err := c.resolve(filePath)
	if err != nil {
		return nil, err
	}

	return os.ReadFile(full)
}

// Exists reports whether an object exists at filePath.
func (c *LocalStorageConnector) Exists(filePath string) (bool, error) {

	full, err := c.resolve(filePath)
	if err != nil {
		return false, err
	}

	if _, err := os.Stat(full); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

// DeleteFile removes a single object. Missing files are not treated as errors.
func (c *LocalStorageConnector) DeleteFile(filePath string) error {

	full, err := c.resolve(filePath)
	if err != nil {
		return err
	}

	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

// DeleteFileWithPrefix removes every file whose key starts with the given prefix.
func (c *LocalStorageConnector) DeleteFileWithPrefix(prefix string) error {

	root, err := filepath.Abs(c.rootDir())
	if err != nil {
		return err
	}

	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	cleanPrefix := strings.TrimLeft(filepath.ToSlash(prefix), "/")

	var toRemove []string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		if strings.HasPrefix(filepath.ToSlash(rel), cleanPrefix) {
			toRemove = append(toRemove, path)
		}

		return nil
	})
	if walkErr != nil {
		return walkErr
	}

	for _, p := range toRemove {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	return nil
}

// PublicURL builds the URL for a key. When base_url is configured it is
// prefixed; otherwise a root-relative path ("/key") is returned.
func (c *LocalStorageConnector) PublicURL(filePath string) string {

	key := strings.TrimLeft(filepath.ToSlash(filePath), "/")

	if base := viper.GetString(c.getConfigPath("base_url")); base != "" {
		return fmt.Sprintf("%s/%s", strings.TrimRight(base, "/"), key)
	}

	return "/" + key
}
