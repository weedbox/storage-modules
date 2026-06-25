package local_storage_connector_test

import (
	"fmt"
	"log"

	"github.com/spf13/viper"
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/weedbox/storage-modules/local_storage_connector"
	"github.com/weedbox/storage-modules/storage_connector"
)

// Example wires the local filesystem backend into a Weedbox / Fx application.
//
// Because Module provides the shared storage_connector.StorageConnector,
// consumers are written against the interface and never reference the concrete
// type — swapping in s3_connector later is a one-line change in the wiring.
func Example() {

	// Configuration is normally loaded from config.toml; set here for clarity.
	viper.Set("local_storage.root_dir", "./data/uploads")
	viper.Set("local_storage.base_url", "https://cdn.example.com")

	app := fx.New(
		fx.Provide(zap.NewDevelopment),
		local_storage_connector.Module("local_storage"),
		fx.Invoke(func(sc storage_connector.StorageConnector) {
			url, err := sc.WriteAsFile("images/logo.png", []byte("...binary..."))
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println(url)
		}),
	)

	_ = app // app.Run() starts the lifecycle in a real program.
}

// ExampleLocalStorageConnector_WriteAsFile writes raw bytes and returns the
// object's access URL. sc is obtained via Fx dependency injection.
func ExampleLocalStorageConnector_WriteAsFile() {

	var sc storage_connector.StorageConnector // injected by Fx

	url, err := sc.WriteAsFile("docs/readme.txt", []byte("hello world"))
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("written to:", url)
}

// ExampleLocalStorageConnector_SaveFile stores base64-encoded content (e.g. from
// a JSON API request). When FileName is empty a UUID is generated automatically.
func ExampleLocalStorageConnector_SaveFile() {

	var sc storage_connector.StorageConnector // injected by Fx

	req := &storage_connector.UploaderReq{
		Category: "avatars",
		FileName: "profile.jpg",                         // optional
		RawData:  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB...", // base64 string
	}

	url, err := sc.SaveFile(req)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("saved to:", url)
}

// ExampleLocalStorageConnector_ReadFile reads an object's full content back.
func ExampleLocalStorageConnector_ReadFile() {

	var sc storage_connector.StorageConnector // injected by Fx

	data, err := sc.ReadFile("docs/readme.txt")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("read %d bytes\n", len(data))
}

// ExampleLocalStorageConnector_DeleteFileWithPrefix removes every file under a
// prefix, for example all files belonging to a single user.
func ExampleLocalStorageConnector_DeleteFileWithPrefix() {

	var sc storage_connector.StorageConnector // injected by Fx

	if err := sc.DeleteFileWithPrefix("avatars/user-123/"); err != nil {
		log.Fatal(err)
	}
}
