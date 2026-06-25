package local_storage_connector

import (
	"encoding/base64"
	"errors"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

func newTestConnector(t *testing.T) *LocalStorageConnector {
	t.Helper()
	viper.Reset()
	c := &LocalStorageConnector{scope: "local", logger: zap.NewNop()}
	c.initDefaultConfigs()
	viper.Set("local.root_dir", t.TempDir())
	return c
}

func TestWriteReadExistsDelete(t *testing.T) {
	c := newTestConnector(t)

	url, err := c.WriteAsFile("docs/readme.txt", []byte("hello"))
	if err != nil {
		t.Fatalf("WriteAsFile: %v", err)
	}
	if url != "/docs/readme.txt" {
		t.Errorf("url = %q, want /docs/readme.txt", url)
	}

	if ok, err := c.Exists("docs/readme.txt"); err != nil || !ok {
		t.Fatalf("Exists = %v, %v; want true, nil", ok, err)
	}

	data, err := c.ReadFile("docs/readme.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("content = %q, want hello", data)
	}

	if err := c.DeleteFile("docs/readme.txt"); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if ok, _ := c.Exists("docs/readme.txt"); ok {
		t.Error("file should not exist after delete")
	}

	// Delete is idempotent.
	if err := c.DeleteFile("docs/readme.txt"); err != nil {
		t.Errorf("idempotent DeleteFile returned error: %v", err)
	}
}

func TestSaveFileBase64(t *testing.T) {
	c := newTestConnector(t)
	raw := base64.StdEncoding.EncodeToString([]byte("image-bytes"))

	url, err := c.SaveFile(&UploaderReq{Category: "avatars", FileName: "a.png", RawData: raw})
	if err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	if url != "/avatars/a.png" {
		t.Errorf("url = %q, want /avatars/a.png", url)
	}

	data, _ := c.ReadFile("avatars/a.png")
	if string(data) != "image-bytes" {
		t.Errorf("content = %q, want image-bytes", data)
	}
}

func TestSaveFileGeneratesUUID(t *testing.T) {
	c := newTestConnector(t)
	raw := base64.StdEncoding.EncodeToString([]byte("x"))

	url, err := c.SaveFile(&UploaderReq{Category: "tmp", RawData: raw})
	if err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	if dir := filepath.Dir(url); dir != "/tmp" {
		t.Errorf("dir(%q) = %q, want /tmp", url, dir)
	}
}

func TestDeleteFileWithPrefix(t *testing.T) {
	c := newTestConnector(t)
	_, _ = c.WriteAsFile("u/123/a.txt", []byte("a"))
	_, _ = c.WriteAsFile("u/123/b.txt", []byte("b"))
	_, _ = c.WriteAsFile("u/456/c.txt", []byte("c"))

	if err := c.DeleteFileWithPrefix("u/123/"); err != nil {
		t.Fatalf("DeleteFileWithPrefix: %v", err)
	}

	if ok, _ := c.Exists("u/123/a.txt"); ok {
		t.Error("u/123/a.txt should be deleted")
	}
	if ok, _ := c.Exists("u/123/b.txt"); ok {
		t.Error("u/123/b.txt should be deleted")
	}
	if ok, _ := c.Exists("u/456/c.txt"); !ok {
		t.Error("u/456/c.txt should remain")
	}
}

func TestPublicURLWithBaseURL(t *testing.T) {
	c := newTestConnector(t)
	viper.Set("local.base_url", "https://cdn.example.com/")

	if got := c.PublicURL("/images/logo.png"); got != "https://cdn.example.com/images/logo.png" {
		t.Errorf("PublicURL = %q", got)
	}
}

func TestResolveRejectsTraversal(t *testing.T) {
	c := newTestConnector(t)

	// A key that escapes the root is rejected with ErrInvalidPath rather than
	// being silently clamped, so it can never reach the real /etc/passwd.
	for _, key := range []string{"../../etc/passwd", "..", "../sibling/file"} {
		if _, err := c.resolve(key); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("resolve(%q) error = %v, want ErrInvalidPath", key, err)
		}
	}

	// The same rejection surfaces through the public API.
	if _, err := c.ReadFile("../../etc/passwd"); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("ReadFile traversal error = %v, want ErrInvalidPath", err)
	}
}

func TestResolveNormalizesInternalDotDot(t *testing.T) {
	c := newTestConnector(t)

	// "a/../b.txt" stays within the root and normalizes to "b.txt"; it must be
	// accepted, not rejected.
	if _, err := c.WriteAsFile("a/../b.txt", []byte("ok")); err != nil {
		t.Fatalf("WriteAsFile(a/../b.txt): %v", err)
	}
	if ok, _ := c.Exists("b.txt"); !ok {
		t.Error("expected b.txt to exist after writing a/../b.txt")
	}
}
