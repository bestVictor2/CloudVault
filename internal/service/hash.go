package service

import (
	"CloudVault/internal/storage"
	"CloudVault/utils"
	"context"
	"fmt"
)

// ComputeObjectHash reads the object from storage and returns its SHA-256 hash and size.
func ComputeObjectHash(ctx context.Context, bucket, object string) (string, int64, error) {
	if storage.Default == nil {
		return "", 0, fmt.Errorf("storage not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	reader, info, err := storage.Default.GetObject(ctx, bucket, object)
	if err != nil {
		return "", 0, err
	}
	defer reader.Close()
	hash, readBytes, err := utils.ComputeSHA256Hex(reader)
	if err != nil {
		return "", 0, err
	}
	if info.Size > 0 && readBytes != info.Size {
		return "", 0, fmt.Errorf("hash read size mismatch")
	}
	return hash, info.Size, nil
}
