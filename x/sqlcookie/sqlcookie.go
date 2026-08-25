// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sqlcookie provides an SQL-backed implementation of [cookie.Storage] for persistent proxy-isolated cookie jars.
package sqlcookie

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lemon4ksan/foundation/codec/json"

	"github.com/lemon4ksan/aoni/cookie"
)

// SQLStorage implements [cookie.Storage] backed by an SQL database (*sql.DB).
// The provided *sql.DB handle MUST be thread-safe for concurrent operations across goroutines.
type SQLStorage struct {
	db        *sql.DB
	tableName string
}

// New instantiates an [SQLStorage] instance using the provided database handle.
func New(db *sql.DB) *SQLStorage {
	return &SQLStorage{
		db:        db,
		tableName: "aoni_cookies",
	}
}

// NewWithTable instantiates an [SQLStorage] instance using the provided database handle and custom table name.
func NewWithTable(db *sql.DB, tableName string) *SQLStorage {
	if tableName == "" {
		tableName = "aoni_cookies"
	}

	return &SQLStorage{
		db:        db,
		tableName: tableName,
	}
}

// InitSchema constructs the required table schema if it does not exist.
func (s *SQLStorage) InitSchema(ctx context.Context) error {
	query := `CREATE TABLE IF NOT EXISTS ` + s.tableName + ` (
		proxy_key TEXT PRIMARY KEY,
		cookie_data TEXT
	);`
	_, err := s.db.ExecContext(ctx, query)

	return err
}

// Save persists cookies associated with key into the SQL database.
func (s *SQLStorage) Save(key string, cookies []cookie.Cookie) error {
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

	if _, err := tx.ExecContext(
		ctx,
		`DELETE FROM `+s.tableName+` WHERE proxy_key = ?`,
		key,
	); err != nil {
		return err
	}

	if len(cookies) > 0 {
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
func (s *SQLStorage) Load(key string) ([]cookie.Cookie, error) {
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

	var cookies []cookie.Cookie
	if err := json.Unmarshal([]byte(dataStr), &cookies); err != nil {
		return nil, cookie.ErrInvalidCookieData
	}

	return cookies, nil
}
