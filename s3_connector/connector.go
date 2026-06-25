package s3_connector

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"
	"github.com/spf13/viper"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

const (
	DefaultBucketName    = "example.com"
	DefaultRegion        = "us-east-1"
	DefaultPresignExpiry = 15 * time.Minute
)

// UploaderReq is the request payload accepted by SaveFile. It mirrors the
// gcp-modules bucket_connector contract so callers can switch backends without
// changing their request shape.
type UploaderReq struct {
	FileName string `json:"file_name"` // Target filename (optional, auto-generates UUID if empty)
	Category string `json:"category"`  // Category/directory path (object key prefix) in the bucket
	RawData  string `json:"rowData"`   // Base64-encoded file content
}

// S3Connector wraps an AWS S3 (or S3-compatible) client and exposes the
// same high-level file operations as the GCP bucket_connector.
type S3Connector struct {
	params        Params
	logger        *zap.Logger
	client        *s3.Client
	presignClient *s3.PresignClient
	scope         string
}

type Params struct {
	fx.In

	Lifecycle fx.Lifecycle
	Logger    *zap.Logger
}

// Module creates an Fx module that provides a *S3Connector. The scope
// parameter namespaces all configuration keys and logger output.
func Module(scope string) fx.Option {

	var m *S3Connector

	return fx.Module(
		scope,
		fx.Provide(func(p Params) *S3Connector {

			m := &S3Connector{
				params: p,
				logger: p.Logger.Named(scope),
				scope:  scope,
			}

			m.initDefaultConfigs()

			return m
		}),
		fx.Populate(&m),
		fx.Invoke(func(p Params) *S3Connector {

			p.Lifecycle.Append(
				fx.Hook{
					OnStart: m.onStart,
					OnStop:  m.onStop,
				},
			)

			return m
		}),
	)
}

func (c *S3Connector) getConfigPath(key string) string {
	return fmt.Sprintf("%s.%s", c.scope, key)
}

func (c *S3Connector) initDefaultConfigs() {
	viper.SetDefault(c.getConfigPath("bucket_name"), DefaultBucketName)
	viper.SetDefault(c.getConfigPath("region"), DefaultRegion)
	viper.SetDefault(c.getConfigPath("endpoint"), "")
	viper.SetDefault(c.getConfigPath("access_key_id"), "")
	viper.SetDefault(c.getConfigPath("secret_access_key"), "")
	viper.SetDefault(c.getConfigPath("use_path_style"), false)
	viper.SetDefault(c.getConfigPath("acl"), "")
	viper.SetDefault(c.getConfigPath("public_base_url"), "")
	viper.SetDefault(c.getConfigPath("presign_expiry"), DefaultPresignExpiry)
}

func (c *S3Connector) onStart(ctx context.Context) error {

	region := viper.GetString(c.getConfigPath("region"))
	endpoint := viper.GetString(c.getConfigPath("endpoint"))
	accessKey := viper.GetString(c.getConfigPath("access_key_id"))
	secretKey := viper.GetString(c.getConfigPath("secret_access_key"))
	usePathStyle := viper.GetBool(c.getConfigPath("use_path_style"))

	c.logger.Info("Starting S3Connector",
		zap.String("bucket_name", c.GetBucketName()),
		zap.String("region", region),
		zap.String("endpoint", endpoint),
		zap.Bool("use_path_style", usePathStyle),
	)

	loadOpts := []func(*config.LoadOptions) error{
		config.WithRegion(region),
	}

	// When static credentials are supplied, use them; otherwise fall back to the
	// default AWS credential chain (env vars, shared config, IAM role, etc.).
	if accessKey != "" && secretKey != "" {
		loadOpts = append(loadOpts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		))
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		c.logger.Error("config.LoadDefaultConfig Error", zap.Error(err))
		return err
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		// A non-empty endpoint targets an S3-compatible service such as
		// Cloudflare R2, MinIO, or GCS interoperability mode.
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		o.UsePathStyle = usePathStyle
	})

	c.client = client
	c.presignClient = s3.NewPresignClient(client)

	return nil
}

func (c *S3Connector) onStop(ctx context.Context) error {
	// The AWS S3 client holds no long-lived connection that requires closing.
	c.logger.Info("Stopped S3Connector")
	return nil
}

// GetBucketName returns the configured bucket name.
func (c *S3Connector) GetBucketName() string {
	return viper.GetString(c.getConfigPath("bucket_name"))
}

// GetClient returns the underlying S3 client for advanced operations.
func (c *S3Connector) GetClient() *s3.Client {
	return c.client
}

// GetPresignClient returns the underlying presign client for advanced operations.
func (c *S3Connector) GetPresignClient() *s3.PresignClient {
	return c.presignClient
}

// WriteAsFile writes raw binary data to the bucket and returns its public URL.
func (c *S3Connector) WriteAsFile(filePath string, content []byte) (string, error) {
	return c.putObject(context.Background(), filePath, bytes.NewReader(content))
}

// SaveFile decodes base64 content and stores it under {category}/{filename}.
// If FileName is empty, a UUID is generated. Returns the public URL.
func (c *S3Connector) SaveFile(req *UploaderReq) (string, error) {

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

	return c.putObject(context.Background(), filePath, bytes.NewReader(data))
}

func (c *S3Connector) putObject(ctx context.Context, filePath string, body io.Reader) (string, error) {

	input := &s3.PutObjectInput{
		Bucket:      aws.String(c.GetBucketName()),
		Key:         aws.String(filePath),
		Body:        body,
		ContentType: aws.String(detectContentType(filePath)),
	}

	// Apply a canned ACL only when explicitly configured. Modern S3 buckets
	// often disable ACLs (Object Ownership = bucket owner enforced), so the
	// default is to set none and rely on bucket policy / presigned URLs.
	if acl := viper.GetString(c.getConfigPath("acl")); acl != "" {
		input.ACL = types.ObjectCannedACL(acl)
	}

	if _, err := c.client.PutObject(ctx, input); err != nil {
		c.logger.Error("PutObject Error", zap.String("key", filePath), zap.Error(err))
		return "", err
	}

	return c.PublicURL(filePath), nil
}

// DeleteFile removes a single object. S3 DeleteObject is idempotent, so a
// missing key is not treated as an error.
func (c *S3Connector) DeleteFile(filePath string) error {

	_, err := c.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
		Bucket: aws.String(c.GetBucketName()),
		Key:    aws.String(filePath),
	})
	if err != nil {
		c.logger.Error("DeleteObject Error", zap.String("key", filePath), zap.Error(err))
		return err
	}

	return nil
}

// DeleteFileWithPrefix removes every object whose key starts with the given
// prefix, batching deletions (up to 1000 keys per request).
func (c *S3Connector) DeleteFileWithPrefix(filePath string) error {

	ctx := context.Background()
	bucket := c.GetBucketName()

	paginator := s3.NewListObjectsV2Paginator(c.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(filePath),
	})

	for paginator.HasMorePages() {

		page, err := paginator.NextPage(ctx)
		if err != nil {
			return err
		}

		if len(page.Contents) == 0 {
			continue
		}

		objects := make([]types.ObjectIdentifier, 0, len(page.Contents))
		for _, obj := range page.Contents {
			objects = append(objects, types.ObjectIdentifier{Key: obj.Key})
		}

		_, err = c.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			Delete: &types.Delete{
				Objects: objects,
				Quiet:   aws.Bool(true),
			},
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// PresignGetURL returns a time-limited signed URL for downloading an object.
// When expiry <= 0, the configured presign_expiry default is used.
func (c *S3Connector) PresignGetURL(filePath string, expiry time.Duration) (string, error) {

	if expiry <= 0 {
		expiry = viper.GetDuration(c.getConfigPath("presign_expiry"))
	}

	req, err := c.presignClient.PresignGetObject(context.Background(),
		&s3.GetObjectInput{
			Bucket: aws.String(c.GetBucketName()),
			Key:    aws.String(filePath),
		},
		s3.WithPresignExpires(expiry),
	)
	if err != nil {
		return "", err
	}

	return req.URL, nil
}

// PresignPutURL returns a time-limited signed URL for uploading an object
// directly from a client. When expiry <= 0, the configured default is used.
func (c *S3Connector) PresignPutURL(filePath string, expiry time.Duration) (string, error) {

	if expiry <= 0 {
		expiry = viper.GetDuration(c.getConfigPath("presign_expiry"))
	}

	req, err := c.presignClient.PresignPutObject(context.Background(),
		&s3.PutObjectInput{
			Bucket: aws.String(c.GetBucketName()),
			Key:    aws.String(filePath),
		},
		s3.WithPresignExpires(expiry),
	)
	if err != nil {
		return "", err
	}

	return req.URL, nil
}

// PublicURL builds the public (unsigned) URL for an object key. Whether the URL
// is actually reachable depends on the bucket's ACL / policy configuration.
func (c *S3Connector) PublicURL(filePath string) string {

	key := strings.TrimLeft(filePath, "/")

	// An explicit public base URL (e.g. a CDN or R2 public domain) wins.
	if base := viper.GetString(c.getConfigPath("public_base_url")); base != "" {
		return fmt.Sprintf("%s/%s", strings.TrimRight(base, "/"), key)
	}

	bucket := c.GetBucketName()
	endpoint := viper.GetString(c.getConfigPath("endpoint"))
	usePathStyle := viper.GetBool(c.getConfigPath("use_path_style"))

	if endpoint != "" {
		if usePathStyle {
			return fmt.Sprintf("%s/%s/%s", strings.TrimRight(endpoint, "/"), bucket, key)
		}

		// Virtual-hosted style on a custom endpoint: prepend the bucket as a
		// subdomain of the endpoint host.
		if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
			u.Host = fmt.Sprintf("%s.%s", bucket, u.Host)
			u.Path = "/" + key
			return u.String()
		}

		// Fallback to path style if the endpoint cannot be parsed.
		return fmt.Sprintf("%s/%s/%s", strings.TrimRight(endpoint, "/"), bucket, key)
	}

	// Standard AWS virtual-hosted-style URL.
	region := viper.GetString(c.getConfigPath("region"))
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", bucket, region, key)
}

func detectContentType(filePath string) string {
	if ct := mime.TypeByExtension(filepath.Ext(filePath)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}
