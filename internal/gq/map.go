package gq

import (
	"iter"
	"maps"
)

type Map[K comparable, V any] map[K]V

func (m Map[K, V]) Len() int {
	return len(m)
}

func (m Map[K, V]) Keys() iter.Seq[K] {
	return maps.Keys(m)
}

func (m Map[K, V]) Values() iter.Seq[V] {
	return maps.Values(m)
}

func (m Map[K, V]) Put(k K, v V) bool {
	if _, ok := m[k]; ok {
		return false
	}
	m.Set(k, v)
	return true
}

func (m Map[K, V]) Get(k K) (V, bool) {
	v, ok := m[k]
	return v, ok
}

func (m Map[K, V]) Set(k K, v V) {
	m[k] = v
}

func (m Map[K, V]) Iterate(act func(K, V) bool) {
	for k, v := range m {
		if !act(k, v) {
			return
		}
	}
}

func (m Map[K, V]) Iter() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for k, v := range m {
			if !yield(k, v) {
				return
			}
		}
	}
}

func (m Map[K, V]) Delete(k K) {
	delete(m, k)
}

func (m Map[K, V]) Clear() {
	clear(m)
}
