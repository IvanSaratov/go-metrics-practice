package retry

import (
	"context"
	"time"

	retrylib "github.com/sethvargo/go-retry"
)

const maxRetries = 3

// Do выполняет операцию с тремя повторами через 1, 3 и 5 секунд.
func Do(ctx context.Context, operation retrylib.RetryFunc) error {
	return retrylib.Do(ctx, newBackoff(), operation)
}

// RetryableError помечает ошибку как временную.
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
