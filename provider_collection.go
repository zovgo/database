package database

import (
	"io"
	"iter"
	"sync"
)

// ProviderCollection is the interface for database provider that stores
// collections of value.s
type ProviderCollection[K comparable, V, DB any] interface {
	io.Closer
	// PutEntry adds an entry to the collection
	// Returns false if an entry with the same model key already exists
	PutEntry(K, V) bool
	// SetEntry adds or replaces an entry in the collection
	SetEntry(K, V)
	// RemoveEntry removes a specific entry from the collection
	RemoveEntry(V)
	// RemoveEntryByID removes a specific entry from the collection by
	// its identification.
	RemoveEntryByID(ownerKey, modelKey K)
	// RemoveEntries removes all entries for a specific owner key
	RemoveEntries(K)
	// LoadEntries returns all entries for a specific owner key
	LoadEntries(key K, clone bool) ([]V, bool)
	// Entries returns iterator of all entries in the collection
	Entries() iter.Seq[V]
	// MapEntries returns key-collection iterator of all provider entries
	MapEntries() iter.Seq2[K, []V]
	// Load initializes the provider collection
	Load()
	// Database returns database instance of this provider.
	Database() *Database[DB]
	// L returns entries mutex (locker) for this provider.
	L() *sync.RWMutex
}
