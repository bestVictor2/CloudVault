package storage

import (
	"CloudVault/config"
	"context"
	"fmt"
	"io"
	"log"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinIOStorage struct {
	Client *minio.Client
	Bucket string
}

type ObjectInfo struct {
	ObjectName string
	Size       int64
}

// MinioStore implements Store with a MinIO client.
type MinioStore struct {
	client *minio.Client
}

// NewMinioStore builds a Store from a MinIO client.
func NewMinioStore(client *minio.Client) *MinioStore {
	return &MinioStore{client: client}
}

// PutObject uploads an object to MinIO.
func (s *MinioStore) PutObject(ctx context.Context, bucket, object string, reader io.Reader, size int64, opts PutOptions) error {
	_, err := s.client.PutObject(ctx, bucket, object, reader, size, minio.PutObjectOptions{
		ContentType: opts.ContentType,
	})
	return err
}

// GetObject fetches an object and its size from MinIO.
func (s *MinioStore) GetObject(ctx context.Context, bucket, object string) (io.ReadCloser, ObjectInfo, error) {
	obj, err := s.client.GetObject(ctx, bucket, object, minio.GetObjectOptions{})
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	stat, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		return nil, ObjectInfo{}, err
	}
	info := ObjectInfo{
		ObjectName: object,
		Size:       stat.Size,
	}
	return obj, info, nil
}

// RemoveObject deletes an object from MinIO.
func (s *MinioStore) RemoveObject(ctx context.Context, bucket, object string) error {
	return s.client.RemoveObject(ctx, bucket, object, minio.RemoveObjectOptions{})
}

// PresignedGetObject returns a presigned URL for downloading an object.
func (s *MinioStore) PresignedGetObject(ctx context.Context, bucket, object string, expiry time.Duration) (string, error) {
	url, err := s.client.PresignedGetObject(ctx, bucket, object, expiry, nil)
	if err != nil {
		return "", err
	}
	return url.String(), nil
}

// PresignedGetObjectWithResponse returns a presigned URL with response headers.
func (s *MinioStore) PresignedGetObjectWithResponse(
	ctx context.Context,
	bucket,
	object string,
	expiry time.Duration,
	params map[string]string,
) (string, error) {
	values := url.Values{}
	for key, value := range params {
		if value == "" {
			continue
		}
		values.Set(key, value)
	}
	url, err := s.client.PresignedGetObject(ctx, bucket, object, expiry, values)
	if err != nil {
		return "", err
	}
	return url.String(), nil
}

// ComposeObject composes multiple source objects into a single destination object.
func (s *MinioStore) ComposeObject(ctx context.Context, dest CopyDest, sources ...CopySource) error {
	srcs := make([]minio.CopySrcOptions, 0, len(sources))
	for _, src := range sources {
		srcs = append(srcs, minio.CopySrcOptions{
			Bucket: src.Bucket,
			Object: src.Object,
		})
	}
	dst := minio.CopyDestOptions{
		Bucket: dest.Bucket,
		Object: dest.Object,
	}
	_, err := s.client.ComposeObject(ctx, dst, srcs...)
	return err
}

var Minio *MinIOStorage
var MinioTest *MinIOStorage

// InitMinio initializes MinIO client and bucket.
func InitMinio() {
	client, err := minio.New(fmt.Sprintf("%s:%s", config.AppConfig.MinioHost, config.AppConfig.MinioPort), &minio.Options{
		Creds:  credentials.NewStaticV4(config.AppConfig.MinioUsername, config.AppConfig.MinioPassword, ""),
		Secure: false,
	})
	if err != nil {
		log.Fatalln("minio error:", err)
	}
	ctx := context.Background()
	exists, err := client.BucketExists(ctx, config.AppConfig.BucketName)
	if err != nil {
		log.Fatalln("check bucket fail:", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, config.AppConfig.BucketName, minio.MakeBucketOptions{}); err != nil {
			log.Fatalln("create bucket fail:", err)
		}
	}
	Minio = &MinIOStorage{
		Client: client,
		Bucket: config.AppConfig.BucketName,
	}
	Default = NewMinioStore(client)
}

// InitMinioTest initializes test MinIO bucket.
func InitMinioTest() {
	client, err := minio.New(fmt.Sprintf("%s:%s", config.AppConfig.MinioHost, config.AppConfig.MinioPort), &minio.Options{
		Creds:  credentials.NewStaticV4(config.AppConfig.MinioUsername, config.AppConfig.MinioPassword, ""),
		Secure: false,
	})
	if err != nil {
		log.Fatalln("minio error:", err)
	}
	ctx := context.Background()
	exists, err := client.BucketExists(ctx, config.AppConfig.BucketNameTest)
	if err != nil {
		log.Fatalln("check bucket fail:", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, config.AppConfig.BucketNameTest, minio.MakeBucketOptions{}); err != nil {
			log.Fatalln("create bucket fail:", err)
		}
	}
	MinioTest = &MinIOStorage{
		Client: client,
		Bucket: config.AppConfig.BucketNameTest,
	}
	DefaultTest = NewMinioStore(client)
}



