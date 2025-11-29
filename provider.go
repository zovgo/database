package database

import (
	"io"
	"iter"
	"sync"
)

// Provider is the implementation of database provider. It loads and saves
// database models once.
// You can read more about it on doc.go.
type Provider[K comparable, V, DB any] interface {
	// Closer only has "Close() error" method.
	// Close method should save modified models to database and close the
	// parent database.
	io.Closer
	CloseUnsafe() error
	Closed() bool
	// PutEntry tries to put entry to the provider memory.
	// It returns false, if there's already an entry by this key.
	PutEntry(K, V) bool
	PutEntryUnsafe(K, V) bool
	// SetEntry updates the entry by key (stores to provider memory).
	// It'll override, if there is an entry by this key
	SetEntry(K, V)
	SetEntryUnsafe(K, V)
	// DeleteEntry deletes entry from the provider by its key.
	DeleteEntry(K)
	DeleteEntryUnsafe(K)
	// LoadEntry tries to load the entry from provider.
	LoadEntry(K) (V, bool)
	LoadEntryUnsafe(K) (V, bool)
	// LoadEntryFunc tries to load the entry from provider by iterating across
	// all entries.
	LoadEntryFunc(yield func(K, V) bool) (V, bool)
	LoadEntryFuncUnsafe(yield func(K, V) bool) (V, bool)
	// Entries returns iterator of all provider entries.
	Entries() iter.Seq[V]
	EntriesUnsafe() iter.Seq[V]
	// MapEntries returns key-value iterator of all provider entries.
	MapEntries() iter.Seq2[K, V]
	MapEntriesUnsafe() iter.Seq2[K, V]
	// Load initializes the provider. It loads all models from the database.
	// It can be only called once.
	Load()
	// Database returns database instance of this provider.
	Database() *Database[DB]
	// L returns entries mutex (locker) for this provider.
	L() *sync.RWMutex
}
