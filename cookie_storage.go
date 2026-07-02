// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"sync"
)

// CookieStorageBackend defines the interface for persisting cookie jar states.
type CookieStorageBackend interface {
	Save(key string, cookies []CookieData) error
	Load(key string) ([]CookieData, error)
}

// JSONFileCookieStorage implements CookieStorageBackend using a single JSON file on disk.
type JSONFileCookieStorage struct {
	mu       sync.Mutex
	filePath string
}

// NewJSONFileCookieStorage creates a new JSONFileCookieStorage at the specified path.
func NewJSONFileCookieStorage(filePath string) *JSONFileCookieStorage {
	return &JSONFileCookieStorage{
		filePath: filePath,
	}
}

type fileStorageData map[string][]CookieData

// Save writes the specified cookies associated with the given key to the JSON file.
func (s *JSONFileCookieStorage) Save(key string, cookies []CookieData) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data := make(fileStorageData)
	//nolint:gosec // Path traversal checked by caller.
	if fileBytes, err := os.ReadFile(s.filePath); err == nil {
		_ = json.Unmarshal(fileBytes, &data)
	}

	data[key] = cookies

	fileBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	//nolint:gosec // Owner-only read/write permissions for cookie security.
	return os.WriteFile(s.filePath, fileBytes, 0o600)
}

// Load reads cookies associated with the given key from the JSON file.
func (s *JSONFileCookieStorage) Load(key string) ([]CookieData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	//nolint:gosec // Path traversal checked by caller.
	fileBytes, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, err
	}

	data := make(fileStorageData)
	if err := json.Unmarshal(fileBytes, &data); err != nil {
		return nil, err
	}

	return data[key], nil
}

// SQLCookieStorage implements CookieStorageBackend using any SQL database (SQLite, Postgres, MySQL).
// It expects a table created with the following schema:
//
// CREATE TABLE IF NOT EXISTS aoni_cookies (
//
//	proxy_key TEXT,
//	cookie_data TEXT,
//	PRIMARY KEY (proxy_key)
//
// );
type SQLCookieStorage struct {
	db        *sql.DB
	tableName string
}

// NewSQLCookieStorage creates a new SQLCookieStorage with a given database connection.
func NewSQLCookieStorage(db *sql.DB) *SQLCookieStorage {
	return &SQLCookieStorage{
		db:        db,
		tableName: "aoni_cookies",
	}
}

// InitSchema creates the required table schema.
func (s *SQLCookieStorage) InitSchema() error {
	//nolint:gosec // Table name is internal and safe.
	query := `CREATE TABLE IF NOT EXISTS ` + s.tableName + ` (
		proxy_key TEXT PRIMARY KEY,
		cookie_data TEXT
	);`
	_, err := s.db.ExecContext(context.Background(), query)

	return err
}

// Save persists the specified cookies to the SQL database.
func (s *SQLCookieStorage) Save(key string, cookies []CookieData) error {
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
func (s *SQLCookieStorage) Load(key string) ([]CookieData, error) {
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

	var cookies []CookieData
	if err := json.Unmarshal([]byte(dataStr), &cookies); err != nil {
		return nil, err
	}

	return cookies, nil
}
