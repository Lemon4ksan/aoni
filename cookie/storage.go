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
	"sync"
)

// Storage defines the interface for persisting cookie jar states.
type Storage interface {
	Save(key string, cookies []Cookie) error
	Load(key string) ([]Cookie, error)
}

// JSONFileStorage implements CookieStorageBackend using a single JSON file on disk.
type JSONFileStorage struct {
	mu       sync.Mutex
	filePath string
	data     fileStorageData
}

// NewJSONFileStorage creates a new JSONFileCookieStorage at the specified path.
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

type fileStorageData map[string][]Cookie

// Save writes the specified cookies associated with the given key to the JSON file.
func (s *JSONFileStorage) Save(key string, cookies []Cookie) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[key] = cookies

	fileBytes, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.filePath, fileBytes, 0o600) //nolint:gosec
}

// Load reads cookies associated with the given key from the JSON file.
func (s *JSONFileStorage) Load(key string) ([]Cookie, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.data[key], nil
}

// SQLStorage implements CookieStorageBackend using any SQL database (SQLite, Postgres, MySQL).
// It expects a table created with the following schema:
//
// CREATE TABLE IF NOT EXISTS aoni_cookies (
//
//	proxy_key TEXT,
//	cookie_data TEXT,
//	PRIMARY KEY (proxy_key)
//
// );
type SQLStorage struct {
	db        *sql.DB
	tableName string
}

// NewSQLStorage creates a new SQLCookieStorage with a given database connection.
func NewSQLStorage(db *sql.DB) *SQLStorage {
	return &SQLStorage{
		db:        db,
		tableName: "aoni_cookies",
	}
}

// InitSchema creates the required table schema.
func (s *SQLStorage) InitSchema() error {
	//nolint:gosec // Table name is internal and safe.
	query := `CREATE TABLE IF NOT EXISTS ` + s.tableName + ` (
		proxy_key TEXT PRIMARY KEY,
		cookie_data TEXT
	);`
	_, err := s.db.ExecContext(context.Background(), query)

	return err
}

// Save persists the specified cookies to the SQL database.
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

	//nolint:gosec // Table name is internal and safe.
	_, err = tx.ExecContext(ctx, `DELETE FROM `+s.tableName+` WHERE proxy_key = ?`, key)
	if err != nil {
		return err
	}

	if len(cookies) > 0 {
		//nolint:gosec // Table name is internal and safe.
		_, err = tx.ExecContext(
			ctx,
			`INSERT INTO `+s.tableName+` (proxy_key, cookie_data) VALUES (?, ?)`,
			key,
			string(jsonData),
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// Load retrieves cookies associated with the given key from the SQL database.
func (s *SQLStorage) Load(key string) ([]Cookie, error) {
	ctx := context.Background()
	//nolint:gosec // Table name is internal and safe.
	row := s.db.QueryRowContext(ctx, `SELECT cookie_data FROM `+s.tableName+` WHERE proxy_key = ?`, key)

	var dataStr string

	err := row.Scan(&dataStr)
	if err != nil {
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
