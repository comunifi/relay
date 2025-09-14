package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/citizenapp2/relay/pkg/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DataDB struct {
	ctx    context.Context
	suffix string
	db     *pgxpool.Pool
	rdb    *pgxpool.Pool
}

// NewDataDB creates a new DB
func NewDataDB(ctx context.Context, db, rdb *pgxpool.Pool, name string) (*DataDB, error) {
	datadb := &DataDB{
		ctx:    ctx,
		suffix: name,
		db:     db,
		rdb:    rdb,
	}

	return datadb, nil
}

// CreateDataTable creates a table to store extra data
func (db *DataDB) CreateDataTable() error {
	_, err := db.db.Exec(db.ctx, fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS t_logs_data_%s(
		hash TEXT NOT NULL PRIMARY KEY,
		data jsonb DEFAULT NULL,
		created_at timestamp NOT NULL DEFAULT current_timestamp,
		updated_at timestamp NOT NULL DEFAULT current_timestamp
	);
	`, db.suffix))

	return err
}

// CreateDataTableIndexes creates the indexes for the data table
func (db *DataDB) CreateDataTableIndexes() error {
	suffix := common.ShortenName(db.suffix, 6)

	_, err := db.db.Exec(db.ctx, fmt.Sprintf(`
	CREATE INDEX IF NOT EXISTS idx_logs_data_%s_hash ON t_logs_data_%s (hash);
	`, suffix, db.suffix))

	return err
}

// UpsertData adds or updates data for a given hash
func (db *DataDB) UpsertData(tx pgx.Tx, hash string, data *json.RawMessage) error {
	_, err := tx.Exec(db.ctx, fmt.Sprintf(`
	INSERT INTO t_logs_data_%s (hash, data, updated_at)
	VALUES ($1, $2, CURRENT_TIMESTAMP)
	ON CONFLICT (hash) 
	DO UPDATE SET 
		data = EXCLUDED.data,
		updated_at = CURRENT_TIMESTAMP
	`, db.suffix), hash, data)

	return err
}

// GetData retrieves data for a given hash
func (db *DataDB) GetData(hash string) (*json.RawMessage, error) {
	var data *json.RawMessage

	err := db.rdb.QueryRow(db.ctx, fmt.Sprintf(`
	SELECT data 
	FROM t_logs_data_%s 
	WHERE hash = $1
	`, db.suffix), hash).Scan(&data)

	return data, err
}
