// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"os"
	"path/filepath"

	"github.com/lemon4ksan/foundation/codec/json"
	"github.com/lemon4ksan/foundation/generic"
)

// ErrInvalidCookieData is returned when persisted cookie data cannot be unmarshaled.
var ErrInvalidCookieData = errors.New("aoni/cookie: invalid persisted cookie payload")

// Storage defines the persistence interface contract for saving and loading proxy-isolated cookie jars.
//
// Thread Safety Requirement:
// Implementations MUST be thread-safe for concurrent read and write operations across goroutines.
type Storage interface {
	Save(key string, cookies []Cookie) error
	Load(key string) ([]Cookie, error)
}

// JSONFileStorage implements thread-safe [Storage] using atomic file writes to disk.
// Writes are performed via temporary file creation and atomic OS file swaps (`os.Rename`),
// guaranteeing zero file corruption even in the event of abrupt process termination.
// Safe for concurrent use across multiple goroutines.
type JSONFileStorage struct {
	filePath string
	data     generic.Safe[fileStorageData]
}

type fileStorageData map[string][]Cookie

// NewJSONFileStorage instantiates a [JSONFileStorage] bound to the specified filePath.
// Automatically loads cookies into memory if filePath exists and contains valid JSON, or initializes empty storage.
func NewJSONFileStorage(filePath string) *JSONFileStorage {
	initialData := make(fileStorageData)
	if fileBytes, err := os.ReadFile(filePath); err == nil {
		_ = json.Unmarshal(fileBytes, &initialData)
	}

	s := &JSONFileStorage{
		filePath: filePath,
		data:     *generic.NewSafe(initialData),
	}

	return s
}

// Save stores cookie slices under key and flushes the JSON payload to disk via atomic temp file swaps.
// Creates parent directories if missing and atomically renames the temporary file to target path.
func (s *JSONFileStorage) Save(key string, cookies []Cookie) error {
	var snapshot fileStorageData

	s.data.Mutate(func(d *fileStorageData) {
		(*d)[key] = cookies

		snapshot = make(fileStorageData, len(*d))
		maps.Copy(snapshot, *d)
	})

	fileBytes, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}

	return writeDataAtomically(s.filePath, fileBytes)
}

// Load retrieves cookies associated with key from memory. Safe for concurrent execution.
func (s *JSONFileStorage) Load(key string) ([]Cookie, error) {
	var copied []Cookie

	s.data.Read(func(d fileStorageData) {
		if cookies, ok := d[key]; ok {
			copied = make([]Cookie, len(cookies))
			copy(copied, cookies)
		}
	})

	return copied, nil
}

// writeDataAtomically writes data to a temporary file in the target directory before renaming it to filePath.
// This guarantees zero partial writes or file corruption on process termination or disk failures.
func writeDataAtomically(filePath string, data []byte) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o750); err != nil { //nolint:gosec
		return err
	}

	tmpFile, err := os.CreateTemp(dir, ".aoni-cookie-*.tmp")
	if err != nil {
		return err
	}

	tmpName := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName) //nolint:gosec
		return err
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpName) //nolint:gosec
		return err
	}

	return os.Rename(tmpName, filePath) //nolint:gosec
}

// SQLStorage implements [Storage] backed by an SQL database (*sql.DB).
// The provided *sql.DB handle MUST be thread-safe for concurrent operations across goroutines.
type SQLStorage struct {
	db        *sql.DB
	tableName string
}

// NewSQLStorage instantiates an [SQLStorage] instance using the provided database handle.
func NewSQLStorage(db *sql.DB) *SQLStorage {
	return &SQLStorage{
		db:        db,
		tableName: "aoni_cookies",
	}
}

// InitSchema constructs the required table schema if it does not exist.
func (s *SQLStorage) InitSchema() error {
	//nolint:gosec
	query := `CREATE TABLE IF NOT EXISTS ` + s.tableName + ` (
		proxy_key TEXT PRIMARY KEY,
		cookie_data TEXT
	);`
	_, err := s.db.ExecContext(context.Background(), query)

	return err
}

// Save persists cookies associated with key into the SQL database.
func (s *SQLStorage) Save(key string, cookies []Cookie) error {
	jsonData, err := json.Marshal(cookies)
	if err != nil {
		return err
	}

	ctx := context.Background()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext( //nolint:gosec
		ctx,
		`DELETE FROM `+s.tableName+` WHERE proxy_key = ?`, //nolint:gosec
		key,
	); err != nil {
		return err
	}

	if len(cookies) > 0 {
		//nolint:gosec
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO `+s.tableName+` (proxy_key, cookie_data) VALUES (?, ?)`,
			key,
			string(jsonData),
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// Load retrieves cookies associated with key from the SQL database.
func (s *SQLStorage) Load(key string) ([]Cookie, error) {
	//nolint:gosec // Table name is validated internally
	row := s.db.QueryRowContext(
		context.Background(),
		`SELECT cookie_data FROM `+s.tableName+` WHERE proxy_key = ?`,
		key,
	)

	var dataStr string
	if err := row.Scan(&dataStr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}

		return nil, err
	}

	var cookies []Cookie
	if err := json.Unmarshal([]byte(dataStr), &cookies); err != nil {
		return nil, ErrInvalidCookieData
	}

	return cookies, nil
}
