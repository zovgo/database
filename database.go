// Package database represents easy to use database without raw SQL, that uses
// sqlite as default.
//
// There are Provider interface and default implementation: ProviderImpl. I
// created it to prevent double code each time (I have multiple providers with
// almost same code). It loads models from the database on initialization of
// the Provider itself, and saves all models (with closing the database) on
// Close method (io.Closer).
package database

import (
	"errors"
	"fmt"
	"github.com/zovgo/database/internal"
	"log/slog"
	"sync/atomic"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Database is easy to use database structure that generally uses *gorm.DB.
type Database[M any] struct {
	l *slog.Logger
	// db is the underlying gorm DB instance.
	db *gorm.DB
	// closed is atomic boolean for database closure.
	closed atomic.Bool
}

// New creates new Database.
// migrateVal should be an instance of the model type for migration.
func New[M any](path string, migrateVal any, l *slog.Logger) (*Database[M], error) {
	conf := &gorm.Config{Logger: logger.Discard, PrepareStmt: true} // Disable GORM's default logger
	db, err := gorm.Open(sqlite.Open(path), conf)
	if err != nil {
		return nil, err
	}
	// Migrate schema using the provided model
	if err := db.AutoMigrate(migrateVal); err != nil {
		return nil, err
	}
	// Enhance logger with database context
	l = l.With("src", "database", "path", path)
	return &Database[M]{l: l, db: db}, nil
}

// Entries returns all database entries.
func (db *Database[M]) Entries() (entries []M) {
	if db.closed.Load() {
		return
	}
	db.checkForErrors("Entries", db.db.Find(&entries))
	return entries
}

// DeleteEntry deletes an entry by queried args.
// Example: db.DeleteEntry("name = ?", "Bob")
// WARNING: If no conditions are provided, it will delete ALL entries!
func (db *Database[M]) DeleteEntry(query string, args ...any) {
	if db.closed.Load() {
		return
	}
	var model M
	db.checkForErrors("DeleteEntry", db.db.Where(query, args...).Delete(&model))
}

// NewEntry creates new entry in database.
func (db *Database[M]) NewEntry(entry M) {
	if db.closed.Load() {
		return
	}
	// Use pointer to entry for proper Create operation
	db.checkForErrors("NewEntry", db.db.Create(&entry))
}

// FindEntry tries to find entry in database by specified arguments.
// Example: db.FindEntry("name = ?", "Bob")
// Returns (entry, true) if found, (zero-value, false) otherwise.
func (db *Database[M]) FindEntry(query string, args ...any) (_ M, _ bool) {
	if db.closed.Load() {
		return
	}
	var model M
	tx := db.db.Where(query, args...).First(&model)
	// Check for errors
	if tx.Error != nil {
		if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			// Not an error, just missing entry
			return model, false
		}
		db.checkForErrors("FindEntry", tx)
		return
	}
	return model, true
}

// UpdateEntry updates entry in database.
// Returns true if update was successful, false if entry not found.
// Uses query and args for finding the record to prevent ambiguous column errors
func (db *Database[M]) UpdateEntry(new M, query string, args ...any) bool {
	if db.closed.Load() {
		return false
	}
	if query == "" {
		db.l.Error("UpdateEntry requires query to prevent mass updates")
		return false
	}
	// Update by model instance to ensure only target record is updated
	return db.checkForErrors("UpdateEntry: apply", db.db.Where(query, args...).Select("*").Updates(new))
}

// Close implements io.Closer.
func (db *Database[M]) Close() error {
	if !db.closed.CompareAndSwap(false, true) {
		return ErrAlreadyClosed
	}
	sqlDB, err := db.db.DB()
	if err != nil {
		return fmt.Errorf("unable to get underlying *sql.DB of *gorm.DB: %w", err)
	}
	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("unable to close database connection: %w", err)
	}
	return nil
}

// Closed ...
func (db *Database[T]) Closed() bool {
	return db.closed.Load()
}

// Logger returns Database logger.
func (db *Database[T]) Logger() *slog.Logger {
	if db.closed.Load() {
		return internal.Discard
	}
	return db.l
}

// checkForErrors checks gorm tx for errors and logs them appropriately.
// Record not found errors are logged as debug, others as errors.
func (db *Database[M]) checkForErrors(method string, tx *gorm.DB) (_ bool) {
	if tx.Error == nil {
		return true
	}

	// Handle record not found separately (not a real error)
	if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
		db.l.Debug("record not found", "method", method)
		return
	}

	// Log actual database errors
	db.l.Error("database operation failed",
		"method", method,
		"err", tx.Error,
		"rows_affected", tx.RowsAffected,
	)

	return
}
