package storage

import (
	"context"
	"io"
	"time"
)

// PutOptions describes upload options for object storage.
type PutOptions struct {
	ContentType string
}

// CopySource describes a source object for server-side composition.
type CopySource struct {
	Bucket string
	Object string
}

// CopyDest describes a destination object for server-side composition.
type CopyDest struct {
	Bucket string
	Object string
}

// ReaderAtSeeker combines reader and seeker for object uploads.
type ReaderAtSeeker interface {
	io.Reader
	io.Seeker
}

// Store abstracts object storage operations.
type Store interface {
	PutObject(ctx context.Context, bucket, object string, reader io.Reader, size int64, opts PutOptions) error
	GetObject(ctx context.Context, bucket, object string) (io.ReadCloser, ObjectInfo, error)
	RemoveObject(ctx context.Context, bucket, object string) error
	PresignedGetObject(ctx context.Context, bucket, object string, expiry time.Duration) (string, error)
	PresignedGetObjectWithResponse(ctx context.Context, bucket, object string, expiry time.Duration, params map[string]string) (string, error)
	ComposeObject(ctx context.Context, dest CopyDest, sources ...CopySource) error
}

// Default is the main object store instance.
var Default Store

// DefaultTest is the test object store instance.
var DefaultTest Store
