package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
)

// ComputeSHA256Hex streams data from reader and returns the hex-encoded SHA-256.
func ComputeSHA256Hex(reader io.Reader) (string, int64, error) {
	hasher := sha256.New()
	written, err := io.Copy(hasher, reader)
	if err != nil {
		return "", written, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), written, nil
}
