package database

import (
	"errors"
	"fmt"
	"github.com/k4ties/gq"
	"github.com/zovgo/database/internal"
	"iter"
	"slices"
	"sync"
	"sync/atomic"
)

//
// I'm not sure what would be better, IdentifiedModel interface (with UUID
// method), or ask user for a function "func(V) K", that gets identifier by
// model by user choice. But for now I'll keep function.
//

// ProviderImpl is default implementation of the Provider.
type ProviderImpl[K comparable, V any, DB any] struct {
	opts ModelOptions[K, V, DB]

	entriesMu sync.RWMutex
	// entries field is all entries of the provider.
	entries gq.Map[K, V]
	// db is the underlying database of this provider.
	db *Database[DB]
	// closed is atomic boolean, that marks if provider is closed.
	closed atomic.Bool

	// once field is once for loading entries from database.
	once sync.Once
}

// ModelOptions is REQUIRED options of the model.
// If you'll not enter them Provider will simply panic.
type ModelOptions[K comparable, V any, DB any] struct {
	// Identify is function that allows user by itself get identifier of this
	// model. Without it, models can't be stored by 'map[key]model'.
	IdentifyModel func(V) K
	// IdentifyDBModel is same function as above, but for DB models.
	IdentifyDBModel func(DB) K
	// ModifyQuery is function, that must return arguments (query) to modify
	// model in the database.
	// Example:
	//
	// func (m myModel) modifyQuery() (string, []any) {
	//    return "uuid = ?", []any{m.MyUUID}
	// }
	ModifyQuery func(DB) (string, []any)
	// CreateDBModel is function that is required to create database models
	// from entries (convert models from Provider memory to database models and
	// save them)
	CreateDBModel func(K, V) DB
	// CreateModel should create default model from DB model.
	CreateModel func(DB) V
	// CompareModels is function to compare two DB models. It is required to
	// check, if we need to update model in the database.
	CompareModels func(DB, DB) bool
	// AlwaysUpdate is boolean that marks, if we should always update all
	// models in the database (without comparing). If it is true, CompareModels
	// can be nil.
	AlwaysUpdate bool
	// NeverUpdate is boolean, that marks if provider should never update the
	// database models. Example: database of archived punishments, we're just
	// adding models without modifying them.
	NeverUpdate bool
	// OnLoad is function that is called right before model is going to be
	// added. It can cancel adding model by returning false in this function.
	OnLoad func(K, *V, DB) bool
}

// validate validates entered options.
// Panics, if something went wrong.
func (opts ModelOptions[K, V, DB]) validate() {
	for _, v := range []struct {
		invalidIf, expected bool
		name                string
	}{
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
		panic(fmt.Errorf("ModelOptions: the subject (%s) is unexpected", v.name))
	}
}

// NewProvider creates new ProviderImpl instance.
// It requires to Database not be closed.
func NewProvider[K comparable, V any, DB any](db *Database[DB], opts ModelOptions[K, V, DB], init bool) Provider[K, V, DB] {
	if db.closed.Load() {
		panic("db is closed, can't create provider")
	}
	opts.validate()
	prov := &ProviderImpl[K, V, DB]{
		entries: make(gq.Map[K, V]),
		db:      db,
		opts:    opts,
	}
	if init {
		prov.Load()
	}
	return prov
}

// LoadEntry ...
func (provider *ProviderImpl[K, V, DB]) LoadEntry(k K) (V, bool) {
	provider.entriesMu.RLock()
	defer provider.entriesMu.RUnlock()
	return provider.entries.Get(k)
}

// SetEntry ...
func (provider *ProviderImpl[K, V, DB]) SetEntry(k K, v V) {
	provider.entriesMu.Lock()
	defer provider.entriesMu.Unlock()
	provider.entries.Set(k, v)
}

// DeleteEntry ...
func (provider *ProviderImpl[K, V, DB]) DeleteEntry(k K) {
	provider.entriesMu.Lock()
	defer provider.entriesMu.Unlock()
	provider.entries.Delete(k)
}

// PutEntry ...
func (provider *ProviderImpl[K, V, DB]) PutEntry(k K, v V) bool {
	provider.entriesMu.Lock()
	defer provider.entriesMu.Unlock()
	return provider.entries.Put(k, v)
}

// Entries ...
func (provider *ProviderImpl[K, V, DB]) Entries() iter.Seq[V] {
	return func(yield func(V) bool) {
		provider.entriesMu.RLock()
		defer provider.entriesMu.RUnlock()
		for _, v := range provider.entries {
			if !yield(v) {
				return
			}
		}
	}
}

// MapEntries ...
func (provider *ProviderImpl[K, V, DB]) MapEntries() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		provider.entriesMu.RLock()
		defer provider.entriesMu.RUnlock()
		for k, v := range provider.entries {
			if !yield(k, v) {
				return
			}
		}
	}
}

// Close ...
func (provider *ProviderImpl[K, V, DB]) Close() error {
	if !provider.closed.CompareAndSwap(false, true) {
		return ErrAlreadyClosed
	}
	func() {
		dbModels := provider.db.Entries()

		provider.entriesMu.Lock()
		defer provider.entriesMu.Unlock()

		if provider.entries.Len() == 0 && len(dbModels) == 0 {
			// No entries to modify.
			return
		}
		if provider.entries.Len() == 0 && len(dbModels) > 0 {
			// Memory has no entries, but db have, so clearing all database
			// entries.
			for _, dbModel := range dbModels {
				query, args := provider.opts.ModifyQuery(dbModel)
				provider.db.DeleteEntry(query, args...)
			}
			return
		}

		if provider.entries.Len() > 0 && len(dbModels) == 0 {
			// Models has entries, but db don't, so creating db entries by
			// memory reference
			for id, entry := range provider.entries {
				raw := provider.opts.CreateDBModel(id, entry)
				// Asserting to the database
				provider.db.NewEntry(raw)
			}
			return
		}

		for id, memoryEntry := range provider.entries {
			if !slices.ContainsFunc(dbModels, func(dbEntry DB) bool {
				return provider.opts.IdentifyDBModel(dbEntry) == provider.opts.IdentifyModel(memoryEntry)
			}) {
				// Entry exists in memory but not exists in the database, so
				// creating it in the db
				provider.db.NewEntry(provider.opts.CreateDBModel(id, memoryEntry))
				continue
			}

			if provider.opts.NeverUpdate {
				continue
			}
			for _, dbModel := range dbModels {
				raw := provider.opts.CreateDBModel(id, memoryEntry)
				if provider.opts.AlwaysUpdate || !provider.opts.CompareModels(raw, dbModel) {
					// Model in memory and model in database aren't equal
					// So, updating it in the database
					query, args := provider.opts.ModifyQuery(dbModel)
					provider.db.UpdateEntry(raw, query, args...)
					continue
				}
			}
		}

		for _, dbModel := range dbModels {
			dbID := provider.opts.IdentifyDBModel(dbModel)
			if _, ok := provider.entries.Get(dbID); !ok {
				// Entry exists in database, but not exists in memory, so,
				// deleting it from the database.
				query, args := provider.opts.ModifyQuery(dbModel)
				provider.db.DeleteEntry(query, args...)
				continue
			}

			// Check, if entry exists in database and not exists in memory
			if !internal.MapContainsFunc(provider.entries, func(k K, v V) bool {
				return k == dbID
			}) {
				// Entry exists in database, but not exists in memory, so,
				// deleting it from the database.
				query, args := provider.opts.ModifyQuery(dbModel)
				provider.db.DeleteEntry(query, args...)
				continue
			}
		}
	}()
	// Closing the database once all models are saved.
	return provider.db.Close()
}

// Load loads all entries from database and stores them to the Provider memory.
// This function must be called on initialize, since it won't lock mutex,
// because user didn't even receive the Provider instance.
func (provider *ProviderImpl[K, V, DB]) Load() {
	provider.once.Do(func() {
		for _, db := range provider.db.Entries() {
			m := provider.opts.CreateModel(db)
			// Get the identifier of this model
			key := provider.opts.IdentifyModel(m)
			// Handle the event
			if fn := provider.opts.OnLoad; fn != nil && !fn(key, &m, db) {
				// Event is canceled.
				continue
			}
			// Store it to the memory
			provider.entries.Set(key, m)
		}
	})
}

// Database ...
func (provider *ProviderImpl[K, V, DB]) Database() *Database[DB] {
	return provider.db
}

var ErrAlreadyClosed = errors.New("already closed")
