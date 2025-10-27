package internal

func MapContainsFunc[K comparable, V any](collection map[K]V, f func(K, V) bool) bool {
	for k, v := range collection {
		if f(k, v) {
			return true
		}
	}
	return false
}
