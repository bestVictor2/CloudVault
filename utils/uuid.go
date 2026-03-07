package utils

import "github.com/google/uuid"

// GetToken returns a random token.
func GetToken() string {
	return uuid.NewString()
}
