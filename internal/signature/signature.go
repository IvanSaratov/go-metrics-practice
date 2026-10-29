// Package signature вычисляет и проверяет подписи HMAC-SHA256.
package signature

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// HeaderName — имя HTTP-заголовка с подписью тела запроса или ответа.
const HeaderName = "HashSHA256"

// Sum вычисляет HMAC-SHA256 и возвращает подпись в hex-формате.
func Sum(value []byte, key string) string {
	return hex.EncodeToString(digest(value, key))
}

// Verify проверяет, что подпись соответствует данным и ключу.
// Если encodedHash содержит некорректный hex, возвращает false.
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
