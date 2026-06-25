package storage_connector

import "testing"

func TestDetectContentType(t *testing.T) {
	cases := map[string]string{
		"photo.png":   "image/png",
		"data.bin":    "application/octet-stream",
		"noextension": "application/octet-stream",
	}
	for path, want := range cases {
		if got := DetectContentType(path); got != want {
			t.Errorf("DetectContentType(%q) = %q, want %q", path, got, want)
		}
	}
}
