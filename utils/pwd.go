package utils

import "golang.org/x/crypto/bcrypt"

// GetPwd hashes a password.
func GetPwd(pwd string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CheckPwd verifies a password hash.
func CheckPwd(pwd string, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(password), []byte(pwd))
	if err != nil {
		return false
	}

	return true
}
