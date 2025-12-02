package utils

import (
	"crypto/rand"
	"math/big"
)

const passwordChars = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789!@#$%"

// GenerateSecurePassword generates a secure random password
func GenerateSecurePassword(length int) (string, error) {
	result := make([]byte, length)
	maxVal := big.NewInt(int64(len(passwordChars)))
	
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, maxVal)
		if err != nil {
			return "", err
		}
		result[i] = passwordChars[n.Int64()]
	}
	
	return string(result), nil
}

// GenerateUUID generates a simple UUID
func GenerateUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return formatUUID(b)
}

func formatUUID(b []byte) string {
	return sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func sprintf(format string, args ...[]byte) string {
	result := make([]byte, 0, 36)
	for i, arg := range args {
		for _, b := range arg {
			result = append(result, hexChar(b>>4), hexChar(b&0x0f))
		}
		if i < len(args)-1 {
			result = append(result, '-')
		}
	}
	return string(result)
}

func hexChar(b byte) byte {
	if b < 10 {
		return '0' + b
	}
	return 'a' + b - 10
}
