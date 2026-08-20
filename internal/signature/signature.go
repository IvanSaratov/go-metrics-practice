package signature

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

const HeaderName = "HashSHA256"

// вычисляет HMAC-SHA256 и возвращает его в hex-формате
func Sum(value []byte, key string) string {
	return hex.EncodeToString(digest(value, key))
}

// проверяет, что подпись соответствует данным и ключу
func Verify(value []byte, key, encodedHash string) bool {
	receivedHash, err := hex.DecodeString(encodedHash)
	if err != nil {
		return false
	}

	// сравниваем подписи
	return hmac.Equal(digest(value, key), receivedHash)
}

// вычисляет бинарную HMAC-SHA256 подпись
func digest(value []byte, key string) []byte {
	hash := hmac.New(sha256.New, []byte(key))
	_, _ = hash.Write(value)
	return hash.Sum(nil)
}
