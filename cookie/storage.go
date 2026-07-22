// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Storage defines the persistence interface for proxy-isolated cookie jars.
type Storage interface {
	Save(key string, cookies []Cookie) error
	Load(key string) ([]Cookie, error)
}

// JSONFileStorage implements [Storage] using a single JSON file on disk.
// Disk writes are serialized using atomic file swaps to prevent data corruption on crash.
type JSONFileStorage struct {
	mu       sync.RWMutex
	filePath string
	data     fileStorageData
}

type fileStorageData map[string][]Cookie

// NewJSONFileStorage creates a [JSONFileStorage] bound to filePath.
func NewJSONFileStorage(filePath string) *JSONFileStorage {
	s := &JSONFileStorage{
		filePath: filePath,
		data:     make(fileStorageData),
	}

	if fileBytes, err := os.ReadFile(filePath); err == nil {
		_ = json.Unmarshal(fileBytes, &s.data)
	}

	return s
}

// Save stores cookies under key and writes the updated JSON file atomically to disk.
func (s *JSONFileStorage) Save(key string, cookies []Cookie) error {
	s.mu.Lock()
	s.data[key] = cookies

	fileBytes, err := json.MarshalIndent(s.data, "", "  ")
	s.mu.Unlock()

	if err != nil {
		return err
	}

	return writeDataAtomically(s.filePath, fileBytes)
}

// Load retrieves cookies associated with key from memory.
func (s *JSONFileStorage) Load(key string) ([]Cookie, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cookies, ok := s.data[key]
	if !ok {
		return nil, nil
	}

	copied := make([]Cookie, len(cookies))
	copy(copied, cookies)

	return copied, nil
}

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

// SQLStorage implements [Storage] using a SQL database connection.
// Requires a table named 'aoni_cookies' with (proxy_key TEXT PRIMARY KEY, cookie_data TEXT).
type SQLStorage struct {
	db        *sql.DB
	tableName string
}

// NewSQLStorage instantiates a [SQLStorage] using the given database instance.
func NewSQLStorage(db *sql.DB) *SQLStorage {
	return &SQLStorage{
		db:        db,
		tableName: "aoni_cookies",
	}
}

// InitSchema creates the required table schema if it does not exist.
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

	//nolint:gosec
	if _, err := tx.ExecContext(
		ctx,
		`DELETE FROM `+s.tableName+` WHERE proxy_key = ?`,
		key,
	); err != nil {
		return err
	}

	if len(cookies) > 0 {
		if _, err := tx.ExecContext( //nolint:gosec
			ctx,
			`INSERT INTO `+s.tableName+` (proxy_key, cookie_data) VALUES (?, ?)`, //nolint:gosec
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
	ctx := context.Background()
	row := s.db.QueryRowContext(ctx, `SELECT cookie_data FROM `+s.tableName+` WHERE proxy_key = ?`, key) //nolint:gosec

	var dataStr string
	if err := row.Scan(&dataStr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}

		return nil, err
	}

	var cookies []Cookie
	if err := json.Unmarshal([]byte(dataStr), &cookies); err != nil {
		return nil, err
	}

	return cookies, nil
}
