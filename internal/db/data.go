package db

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DataDB struct {
	ctx context.Context
	db  *pgxpool.Pool
	rdb *pgxpool.Pool
}

// NewDataDB creates a new DB
func NewDataDB(ctx context.Context, db, rdb *pgxpool.Pool) (*DataDB, error) {
	datadb := &DataDB{
		ctx: ctx,
		db:  db,
		rdb: rdb,
	}

	return datadb, nil
}

// CreateDataTable creates a table to store extra data
func (db *DataDB) CreateDataTable() error {
	_, err := db.db.Exec(db.ctx, `
	CREATE TABLE IF NOT EXISTS t_logs_data(
		hash TEXT NOT NULL PRIMARY KEY,
		data jsonb DEFAULT NULL,
		created_at timestamp NOT NULL DEFAULT current_timestamp,
		updated_at timestamp NOT NULL DEFAULT current_timestamp
	);`)

	return err
}

// CreateDataTableIndexes creates the indexes for the data table
func (db *DataDB) CreateDataTableIndexes() error {
	_, err := db.db.Exec(db.ctx, `
	CREATE INDEX IF NOT EXISTS idx_logs_data_hash ON t_logs_data (hash);
	`)

	return err
}

// UpsertData adds or updates data for a given hash
func (db *DataDB) UpsertData(hash string, data *json.RawMessage) error {
	_, err := db.db.Exec(db.ctx, `
	INSERT INTO t_logs_data (hash, data, updated_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)
		ON CONFLICT (hash) 
		DO UPDATE SET 
			data = EXCLUDED.data,
			updated_at = CURRENT_TIMESTAMP
	`, hash, data)

	return err
}

// GetData retrieves data for a given hash
func (db *DataDB) GetData(hash string) (*json.RawMessage, error) {
	var data *json.RawMessage

	err := db.rdb.QueryRow(db.ctx, `
	SELECT data 
	FROM t_logs_data 
	WHERE hash = $1
	`, hash).Scan(&data)

	return data, err
}

// DeleteData deletes data for a given hash
func (db *DataDB) DeleteData(hash string) error {
	_, err := db.db.Exec(db.ctx, `
	DELETE FROM t_logs_data WHERE hash = $1
	`, hash)

	return err
}
