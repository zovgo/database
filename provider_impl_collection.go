package database

import (
	"fmt"
	"github.com/k4ties/gq"
	"iter"
	"slices"
	"sync"
	"sync/atomic"
)

// ProviderCollectionImpl is default implementation of the ProviderCollection.
type ProviderCollectionImpl[K comparable, V any, DB any] struct {
	opts ModelCollectionOptions[K, V, DB]

	entriesMu sync.RWMutex
	// entries field is all entries grouped by owner key
	entries gq.Map[K, []V]
	// db is the underlying database of this provider
	db *Database[DB]
	// closed is atomic boolean, that marks if provider is closed
	closed atomic.Bool

	// once field is once for loading entries from database
	once sync.Once
}

// ModelCollectionOptions is REQUIRED options of the model collection
type ModelCollectionOptions[K comparable, V any, DB any] struct {
	// IdentifyOwner is function that returns the owner key for grouping models
	IdentifyOwner func(V) K
	// IdentifyModel is function that returns unique identifier for each model
	IdentifyModel func(V) K
	// IdentifyDBModel is same function as above, but for DB models
	IdentifyDBModel func(DB) K
	// ModifyQuery is function that must return arguments (query) to modify
	// model in the database
	ModifyQuery func(DB) (string, []any)
	// CreateDBModel is function that creates database models from entries
	CreateDBModel func(K, V) DB
	// CreateModel should create default model from DB model
	CreateModel func(DB) V
	// CompareModels is function to compare two DB models
	CompareModels func(DB, DB) bool
	// AlwaysUpdate marks if we should always update all models
	AlwaysUpdate bool
	// NeverUpdate marks if provider should never update the database models
	NeverUpdate bool
	// OnLoad is function that is called right before model is going to be
	// added. It can cancel adding model by returning false in this function.
	OnLoad func(ownerKey K, model *V, dbModel DB) bool
}

// validate validates entered options
func (opts ModelCollectionOptions[K, V, DB]) validate() {
	for _, v := range []struct {
		invalidIf, expected bool
		name                string
	}{
		{invalidIf: opts.IdentifyOwner == nil, name: "IdentifyOwner func is nil"},
		{invalidIf: opts.IdentifyModel == nil, name: "IdentifyModel func is nil"},
		{invalidIf: opts.IdentifyDBModel == nil, name: "IdentifyDBModel func is nil"},
		{invalidIf: opts.ModifyQuery == nil, name: "ModifyQuery func is nil"},
		{invalidIf: opts.CreateDBModel == nil, name: "CreateDBModel func is nil"},
		{invalidIf: opts.CreateModel == nil, name: "CreateModel func is nil"},
		{invalidIf: !opts.AlwaysUpdate && opts.CompareModels == nil, name: "CompareModels func is nil and not AlwaysUpdate"},
	} {
		if v.invalidIf == v.expected {
			continue
		}
		panic(fmt.Errorf("ModelCollectionOptions: the subject (%s) is unexpected", v.name))
	}
}

// NewProviderCollection creates new ProviderCollectionImpl instance
func NewProviderCollection[K comparable, V any, DB any](db *Database[DB], opts ModelCollectionOptions[K, V, DB], init bool) ProviderCollection[K, V, DB] {
	if db.closed.Load() {
		panic("db is closed, can't create provider collection")
	}
	opts.validate()
	prov := &ProviderCollectionImpl[K, V, DB]{
		entries: make(gq.Map[K, []V]),
		db:      db,
		opts:    opts,
	}
	if init {
		prov.Load()
	}
	return prov
}

// LoadEntries ...
func (provider *ProviderCollectionImpl[K, V, DB]) LoadEntries(ownerKey K) ([]V, bool) {
	if provider.closed.Load() {
		return nil, false
	}
	provider.entriesMu.RLock()
	defer provider.entriesMu.RUnlock()
	entries, exists := provider.entries.Get(ownerKey)
	if !exists {
		return nil, false
	}
	cloned := slices.Clone(entries)
	return cloned, true
}

// PutEntry ...
func (provider *ProviderCollectionImpl[K, V, DB]) PutEntry(ownerKey K, v V) bool {
	if provider.closed.Load() {
		return false
	}
	modelKey := provider.opts.IdentifyModel(v)

	provider.entriesMu.Lock()
	defer provider.entriesMu.Unlock()

	entries, exists := provider.entries.Get(ownerKey)
	if !exists {
		provider.entries.Set(ownerKey, []V{v})
		return true
	}

	// Check if entry with same model key already exists
	for _, entry := range entries {
		if provider.opts.IdentifyModel(entry) == modelKey {
			return false
		}
	}

	entries = append(entries, v)
	provider.entries.Set(ownerKey, entries)
	return true
}

// SetEntry ...
func (provider *ProviderCollectionImpl[K, V, DB]) SetEntry(ownerKey K, v V) {
	if provider.closed.Load() {
		return
	}
	modelKey := provider.opts.IdentifyModel(v)

	provider.entriesMu.Lock()
	defer provider.entriesMu.Unlock()

	entries, exists := provider.entries.Get(ownerKey)
	if !exists {
		provider.entries.Set(ownerKey, []V{v})
		return
	}

	var found bool
	for i, entry := range entries {
		// Replace existing entry or append new one
		if provider.opts.IdentifyModel(entry) == modelKey {
			entries[i] = v
			found = true
			break
		}
	}

	if !found {
		entries = append(entries, v)
	}

	provider.entries.Set(ownerKey, entries)
}

// RemoveEntry ...
func (provider *ProviderCollectionImpl[K, V, DB]) RemoveEntry(v V) {
	if provider.closed.Load() {
		return
	}
	ownerKey := provider.opts.IdentifyOwner(v)
	modelKey := provider.opts.IdentifyModel(v)
	provider.RemoveEntryByID(ownerKey, modelKey)
}

// RemoveEntryByID ...
func (provider *ProviderCollectionImpl[K, V, DB]) RemoveEntryByID(ownerKey, modelKey K) {
	if provider.closed.Load() {
		return
	}
	provider.removeEntry(ownerKey, modelKey)
}

func (provider *ProviderCollectionImpl[K, V, DB]) removeEntry(ownerKey, modelKey K) {
	if provider.closed.Load() {
		return
	}
	provider.entriesMu.Lock()
	defer provider.entriesMu.Unlock()

	entries, exists := provider.entries.Get(ownerKey)
	if !exists {
		return
	}

	for i, entry := range entries {
		if provider.opts.IdentifyModel(entry) == modelKey {
			entries = append(entries[:i], entries[i+1:]...)
			if len(entries) == 0 {
				provider.entries.Delete(ownerKey)
			} else {
				provider.entries.Set(ownerKey, entries)
			}
			return
		}
	}
}

// RemoveEntries ...
func (provider *ProviderCollectionImpl[K, V, DB]) RemoveEntries(ownerKey K) {
	if provider.closed.Load() {
		return
	}
	provider.entriesMu.Lock()
	defer provider.entriesMu.Unlock()
	provider.entries.Delete(ownerKey)
}

// Entries ...
func (provider *ProviderCollectionImpl[K, V, DB]) Entries() iter.Seq[V] {
	if provider.closed.Load() {
		return func(yield func(V) bool) {}
	}
	return func(yield func(V) bool) {
		provider.entriesMu.RLock()
		defer provider.entriesMu.RUnlock()
		for _, entries := range provider.entries {
			for _, v := range entries {
				if !yield(v) {
					return
				}
			}
		}
	}
}

// MapEntries ...
func (provider *ProviderCollectionImpl[K, V, DB]) MapEntries() iter.Seq2[K, []V] {
	if provider.closed.Load() {
		return func(yield func(K, []V) bool) {}
	}
	return func(yield func(K, []V) bool) {
		provider.entriesMu.RLock()
		defer provider.entriesMu.RUnlock()
		for k, entries := range provider.entries {
			copied := slices.Clone(entries)
			if !yield(k, copied) {
				return
			}
		}
	}
}

// Close ...
func (provider *ProviderCollectionImpl[K, V, DB]) Close() error {
	if !provider.closed.CompareAndSwap(false, true) {
		return ErrAlreadyClosed
	}

	func() {
		dbModels := provider.db.Entries()

		provider.entriesMu.Lock()
		defer provider.entriesMu.Unlock()

		var allMemoryEntries []V
		for _, entries := range provider.entries {
			allMemoryEntries = append(allMemoryEntries, entries...)
		}

		if len(allMemoryEntries) == 0 && len(dbModels) == 0 {
			// No entries to modify
			return
		}

		if len(allMemoryEntries) == 0 && len(dbModels) > 0 {
			// Memory has no entries, but db has, so clearing all database entries
			for _, dbModel := range dbModels {
				query, args := provider.opts.ModifyQuery(dbModel)
				provider.db.DeleteEntry(query, args...)
			}
			return
		}

		if len(allMemoryEntries) > 0 && len(dbModels) == 0 {
			// Models has entries, but db doesn't, so creating db entries from memory
			for _, entry := range allMemoryEntries {
				id := provider.opts.IdentifyModel(entry)
				raw := provider.opts.CreateDBModel(id, entry)
				provider.db.NewEntry(raw)
			}
			return
		}

		// Sync memory entries with database entries
		// First, handle updates and creations
		for _, memoryEntry := range allMemoryEntries {
			memoryKey := provider.opts.IdentifyModel(memoryEntry)
			// Find this entry in database
			var (
				matchingDBModel DB
				found           bool
			)
			for _, dbModel := range dbModels {
				if provider.opts.IdentifyDBModel(dbModel) == memoryKey {
					matchingDBModel = dbModel
					found = true
					break
				}
			}
			if found {
				if provider.opts.NeverUpdate {
					// Skip update if NeverUpdate is set
					continue
				}

				// Entry exists in both memory and database
				raw := provider.opts.CreateDBModel(memoryKey, memoryEntry)
				if provider.opts.AlwaysUpdate || !provider.opts.CompareModels(raw, matchingDBModel) {
					// Model in memory and model in database aren't equal or AlwaysUpdate is set
					// So, updating it in the database
					query, args := provider.opts.ModifyQuery(matchingDBModel)
					provider.db.UpdateEntry(raw, query, args...)
				}
			} else {
				// Entry exists in memory but not in database, so creating it
				raw := provider.opts.CreateDBModel(memoryKey, memoryEntry)
				provider.db.NewEntry(raw)
			}
		}
		// Then, delete entries that exist in database but not in memory
		for _, dbModel := range dbModels {
			dbKey := provider.opts.IdentifyDBModel(dbModel)
			// Check if this database entry exists in memory
			var found bool
			for _, memoryEntry := range allMemoryEntries {
				if provider.opts.IdentifyModel(memoryEntry) == dbKey {
					found = true
					break
				}
			}
			if !found {
				// Entry exists in database but not in memory, so deleting it
				query, args := provider.opts.ModifyQuery(dbModel)
				provider.db.DeleteEntry(query, args...)
			}
		}
	}()
	return provider.db.Close()
}

// Load ...
func (provider *ProviderCollectionImpl[K, V, DB]) Load() {
	provider.once.Do(func() {
		for _, db := range provider.db.Entries() {
			m := provider.opts.CreateModel(db)
			ownerKey := provider.opts.IdentifyOwner(m)

			// Handle the event
			if fn := provider.opts.OnLoad; fn != nil && !fn(ownerKey, &m, db) {
				// Event is canceled
				continue
			}

			provider.entriesMu.Lock()
			entries, _ := provider.entries.Get(ownerKey)
			entries = append(entries, m)
			provider.entries.Set(ownerKey, entries)
			provider.entriesMu.Unlock()
		}
	})
}

// Database ...
func (provider *ProviderCollectionImpl[K, V, DB]) Database() *Database[DB] {
	return provider.db
}
