// Package retry повторяет операции при временных ошибках.
package retry

import (
	"context"
	"time"

	retrylib "github.com/sethvargo/go-retry"
)

const maxRetries = 3

// Do выполняет операцию и при временной ошибке повторяет её до трёх раз.
// Паузы между попытками — 1, 3 и 5 секунд.
// Для повтора ошибка должна быть помечена через RetryableError.
// Отмена ctx прерывает ожидание следующей попытки.
func Do(ctx context.Context, operation retrylib.RetryFunc) error {
	return retrylib.Do(ctx, newBackoff(), operation)
}

// RetryableError помечает ошибку для повторной попытки в Do.
// Для nil возвращает nil.
func RetryableError(err error) error {
	return retrylib.RetryableError(err)
}

func newBackoff() retrylib.Backoff {
	var retryNumber uint64

	backoff := retrylib.BackoffFunc(func() (time.Duration, bool) {
		retryNumber++
		return time.Duration(2*retryNumber-1) * time.Second, false
	})

	return retrylib.WithMaxRetries(maxRetries, backoff)
}
