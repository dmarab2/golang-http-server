package auth

import (
	"crypto/rand"
	"encoding/hex"
)

func MakeRefreshToken() (string, error) {
	key := make([]byte, 32)
	rand.Read(key)
	encodedString := hex.EncodeToString(key)
	return encodedString, nil
}
