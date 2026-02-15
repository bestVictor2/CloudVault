package utils

import (
	"crypto/rand"
	"math/big"
)

// GenExtractCode generates a share extract code.
func GenExtractCode() string {
	chars := "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	code := make([]byte, 4)
	for i := range code {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			code[i] = chars[0]
			continue
		}
		code[i] = chars[n.Int64()]
	}
	return string(code)
}
