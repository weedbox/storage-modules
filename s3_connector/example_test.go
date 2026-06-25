package s3_connector_test

import (
	"fmt"
	"log"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/weedbox/storage-modules/s3_connector"
)

// Example shows the typical way to wire the connector into a Weedbox / Fx
// application and use it from another component.
func Example() {

	// Configuration is normally loaded from config.toml; set here for clarity.
	viper.Set("s3_storage.bucket_name", "my-bucket")
	viper.Set("s3_storage.region", "ap-northeast-1")
	viper.Set("s3_storage.access_key_id", "AKIA...")
	viper.Set("s3_storage.secret_access_key", "...")

	app := fx.New(
		fx.Provide(zap.NewDevelopment),
		s3_connector.Module("s3_storage"),
		fx.Invoke(func(bc *s3_connector.S3Connector) {
			url, err := bc.WriteAsFile("images/logo.png", []byte("...binary..."))
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println(url)
		}),
	)

	_ = app // app.Run() starts the lifecycle in a real program.
}

// ExampleS3Connector_WriteAsFile uploads raw bytes and returns the object's
// public URL. bc is obtained via Fx dependency injection.
func ExampleS3Connector_WriteAsFile() {

	var bc *s3_connector.S3Connector // injected by Fx

	content := []byte("hello world")

	url, err := bc.WriteAsFile("docs/readme.txt", content)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("uploaded to:", url)
}

// ExampleS3Connector_SaveFile uploads base64-encoded content (e.g. from a JSON
// API request). When FileName is empty a UUID is generated automatically.
func ExampleS3Connector_SaveFile() {

	var bc *s3_connector.S3Connector // injected by Fx

	req := &s3_connector.UploaderReq{
		Category: "avatars",
		FileName: "profile.jpg",                         // optional
		RawData:  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB...", // base64 string
	}

	url, err := bc.SaveFile(req)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("saved to:", url)
}

// ExampleS3Connector_PresignGetURL generates a time-limited download link for a
// private object, so no public bucket access is required.
func ExampleS3Connector_PresignGetURL() {

	var bc *s3_connector.S3Connector // injected by Fx

	// Valid for 10 minutes; pass 0 to use the configured presign_expiry default.
	link, err := bc.PresignGetURL("avatars/profile.jpg", 10*time.Minute)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("download link:", link)
}

// ExampleS3Connector_PresignPutURL generates a signed URL the browser/client can
// PUT to directly, letting end users upload without routing bytes through the
// backend.
func ExampleS3Connector_PresignPutURL() {

	var bc *s3_connector.S3Connector // injected by Fx

	uploadURL, err := bc.PresignPutURL("uploads/user-123/photo.png", 15*time.Minute)
	if err != nil {
		log.Fatal(err)
	}

	// Hand uploadURL to the client; it then does:
	//   curl -X PUT --upload-file ./photo.png "<uploadURL>"
	fmt.Println("upload to:", uploadURL)
}

// ExampleS3Connector_DeleteFile removes a single object. The operation is
// idempotent, so deleting a missing key is not an error.
func ExampleS3Connector_DeleteFile() {

	var bc *s3_connector.S3Connector // injected by Fx

	if err := bc.DeleteFile("docs/readme.txt"); err != nil {
		log.Fatal(err)
	}
}

// ExampleS3Connector_DeleteFileWithPrefix removes every object under a prefix,
// for example all files belonging to a single user.
func ExampleS3Connector_DeleteFileWithPrefix() {

	var bc *s3_connector.S3Connector // injected by Fx

	if err := bc.DeleteFileWithPrefix("avatars/user-123/"); err != nil {
		log.Fatal(err)
	}
}
