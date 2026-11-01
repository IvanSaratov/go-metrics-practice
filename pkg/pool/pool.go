package pool

import "sync"

// Pool хранит объекты для повторного использования из разных горутин
type Pool[T interface{ Reset() }] struct {
	objects sync.Pool
}

// New создаёт пул с функцией создания новых объектов.
func New[T interface{ Reset() }](newObject func() T) *Pool[T] {
	if newObject == nil {
		panic("pool: nil factory")
	}

	return &Pool[T]{
		objects: sync.Pool{
			New: func() any { return newObject() },
		},
	}
}

// Get возвращает объект из пула или создаёт новый, если готового нет.
func (p *Pool[T]) Get() T {
	return p.objects.Get().(T)
}

// Put сбрасывает ненулевой объект и возвращает его в пул.
func (p *Pool[T]) Put(value T) {
	value.Reset()
	p.objects.Put(value)
}
