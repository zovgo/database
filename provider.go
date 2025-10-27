package database

import (
	"io"
	"iter"
)

// Provider is the implementation of database provider. It loads and saves
// database models once.
// You can read more about it on doc.go.
type Provider[K comparable, V, DB any] interface {
	// Closer only has "Close() error" method.
	// Close method should save modified models to database and close the
	// parent database.
	io.Closer
	// PutEntry tries to put entry to the provider memory.
	// It returns false, if there's already an entry by this key.
	PutEntry(K, V) bool
	// SetEntry updates the entry by key (stores to provider memory).
	// It'll override, if there is an entry by this key
	SetEntry(K, V)
	// DeleteEntry deletes entry from the provider by its key.
	DeleteEntry(K)
	// LoadEntry tries to load the entry from provider.
	LoadEntry(K) (V, bool)
	// Entries returns iterator of all provider entries.
	Entries() iter.Seq[V]
	// MapEntries returns key-value iterator of all provider entries.
	MapEntries() iter.Seq2[K, V]
	// Load initializes the provider. It loads all models from the database.
	// It can be only called once.
	Load()
	// Database returns database instance of this provider.
	Database() *Database[DB]
}
