package pool_test

import (
	"sync"
	"testing"

	"github.com/IvanSaratov/go-metrics-practice/internal/model"
	"github.com/IvanSaratov/go-metrics-practice/pkg/pool"
	"github.com/stretchr/testify/require"
)

type testObject struct {
	value      int
	resetCalls int
}

func (o *testObject) Reset() {
	o.value = 0
	o.resetCalls++
}

func TestNewCreatesLazily(t *testing.T) {
	calls := 0
	p := pool.New(func() *testObject {
		calls++
		return &testObject{value: 42}
	})
	require.Zero(t, calls)

	object := p.Get()
	require.NotNil(t, object)
	require.Equal(t, 1, calls)
	require.Equal(t, 42, object.value)
	require.Zero(t, object.resetCalls)
}

func TestPutResetsObject(t *testing.T) {
	p := pool.New(func() *testObject { return &testObject{} })
	object := p.Get()
	object.value = 42

	p.Put(object)

	// Проверяем сброс сразу: следующий Get может вернуть другой объект.
	require.Zero(t, object.value)
	require.Equal(t, 1, object.resetCalls)
}

func TestNewRejectsNilFactory(t *testing.T) {
	require.Panics(t, func() {
		pool.New[*testObject](nil)
	})
}

func TestPoolConcurrent(t *testing.T) {
	p := pool.New(func() *testObject { return &testObject{} })
	const workers = 8
	failures := make(chan string, workers)
	var wg sync.WaitGroup

	for worker := range workers {
		wg.Go(func() {
			for range 100 {
				object := p.Get()
				if object == nil {
					failures <- "пул вернул nil"
					return
				}
				if object.value != 0 {
					failures <- "пул вернул несброшенный объект"
					return
				}
				object.value = worker + 1
				p.Put(object)
			}
		})
	}
	wg.Wait()
	close(failures)

	// Ошибки из горутин проверяем после завершения их работы.
	for message := range failures {
		t.Error(message)
	}
}

func TestPoolWithMetrics(t *testing.T) {
	p := pool.New(func() *model.Metrics {
		return &model.Metrics{Delta: new(int64), Value: new(float64)}
	})
	metric := p.Get()
	delta, value := metric.Delta, metric.Value
	metric.ID = "metric"
	metric.MType = model.Gauge
	metric.Hash = "hash"
	*delta = 10
	*value = 23.5

	p.Put(metric)

	require.Empty(t, metric.ID)
	require.Empty(t, metric.MType)
	require.Empty(t, metric.Hash)
	require.Same(t, delta, metric.Delta)
	require.Same(t, value, metric.Value)
	require.Zero(t, *delta)
	require.Zero(t, *value)

	// Пул может вернуть новый экземпляр, но его состояние тоже должно быть чистым.
	next := p.Get()
	require.NotNil(t, next)
	require.NotNil(t, next.Delta)
	require.NotNil(t, next.Value)
	require.Empty(t, next.ID)
	require.Empty(t, next.MType)
	require.Empty(t, next.Hash)
	require.Zero(t, *next.Delta)
	require.Zero(t, *next.Value)
	p.Put(next)
}
