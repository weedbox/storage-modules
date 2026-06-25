package s3_connector

import (
	"testing"

	"github.com/spf13/viper"
)

func newTestConnector(scope string) *S3Connector {
	c := &S3Connector{scope: scope}
	c.initDefaultConfigs()
	return c
}

func TestPublicURL_StandardAWS(t *testing.T) {
	viper.Reset()
	c := newTestConnector("s3")
	viper.Set("s3.bucket_name", "my-bucket")
	viper.Set("s3.region", "ap-northeast-1")

	got := c.PublicURL("avatars/profile.jpg")
	want := "https://my-bucket.s3.ap-northeast-1.amazonaws.com/avatars/profile.jpg"
	if got != want {
		t.Errorf("PublicURL = %q, want %q", got, want)
	}
}

func TestPublicURL_PublicBaseURL(t *testing.T) {
	viper.Reset()
	c := newTestConnector("s3")
	viper.Set("s3.bucket_name", "my-bucket")
	viper.Set("s3.public_base_url", "https://cdn.example.com/")

	got := c.PublicURL("/images/photo.png")
	want := "https://cdn.example.com/images/photo.png"
	if got != want {
		t.Errorf("PublicURL = %q, want %q", got, want)
	}
}

func TestPublicURL_CustomEndpointPathStyle(t *testing.T) {
	viper.Reset()
	c := newTestConnector("s3")
	viper.Set("s3.bucket_name", "my-bucket")
	viper.Set("s3.endpoint", "http://localhost:9000")
	viper.Set("s3.use_path_style", true)

	got := c.PublicURL("dir/file.txt")
	want := "http://localhost:9000/my-bucket/dir/file.txt"
	if got != want {
		t.Errorf("PublicURL = %q, want %q", got, want)
	}
}

func TestPublicURL_CustomEndpointVirtualHosted(t *testing.T) {
	viper.Reset()
	c := newTestConnector("s3")
	viper.Set("s3.bucket_name", "my-bucket")
	viper.Set("s3.endpoint", "https://r2.example.com")
	viper.Set("s3.use_path_style", false)

	got := c.PublicURL("dir/file.txt")
	want := "https://my-bucket.r2.example.com/dir/file.txt"
	if got != want {
		t.Errorf("PublicURL = %q, want %q", got, want)
	}
}
