package logdb

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	ctx context.Context

	chainID *big.Int
	mu      sync.Mutex
	db      *pgxpool.Pool
	rdb     *pgxpool.Pool

	EventDB *EventDB
	LogDB   *LogDB
}

// NewDB instantiates a new DB
func NewDB(chainID *big.Int, secret, username, password, dbname, port, host, rhost string) (*DB, error) {
	ctx := context.Background()

	connStr := fmt.Sprintf("user=%s password=%s dbname=%s host=%s port=%s sslmode=disable", username, password, dbname, host, port)
	db, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	err = db.Ping(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	evname := chainID.String()

	eventDB, err := NewEventDB(ctx, db, db, evname)
	if err != nil {
		return nil, err
	}

	datadb, err := NewDataDB(ctx, db, db, evname)
	if err != nil {
		return nil, err
	}

	logDB, err := NewLogDB(ctx, db, db, evname, datadb)
	if err != nil {
		return nil, err
	}

	d := &DB{
		ctx:     ctx,
		chainID: chainID,
		db:      db,
		rdb:     db,
		EventDB: eventDB,
		LogDB:   logDB,
	}

	return d, nil
}

// EventTableExists checks if a table exists in the database
func (db *DB) EventTableExists(suffix string) (bool, error) {
	tableName := fmt.Sprintf("t_events_%s", suffix)
	var exists bool
	err := db.rdb.QueryRow(db.ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)", tableName).Scan(&exists)
	if err != nil {
		// A database error occurred
		return false, err
	}
	return exists, nil
}

// SponsorTableExists checks if a table exists in the database
func (db *DB) SponsorTableExists(suffix string) (bool, error) {
	tableName := fmt.Sprintf("t_sponsors_%s", suffix)
	var exists bool
	err := db.rdb.QueryRow(db.ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)", tableName).Scan(&exists)
	if err != nil {
		// A database error occurred
		return false, err
	}
	return exists, nil
}

// LogTableExists checks if a table exists in the database
func (db *DB) LogTableExists(suffix string) (bool, error) {
	tableName := fmt.Sprintf("t_transfers_%s", suffix)
	var exists bool
	err := db.rdb.QueryRow(db.ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)", tableName).Scan(&exists)
	if err != nil {
		// A database error occurred
		return false, err
	}
	return exists, nil
}

// PushTokenTableExists checks if a table exists in the database
func (db *DB) PushTokenTableExists(suffix string) (bool, error) {
	tableName := fmt.Sprintf("t_push_token_%s", suffix)
	var exists bool
	err := db.rdb.QueryRow(db.ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)", tableName).Scan(&exists)
	if err != nil {
		// A database error occurred
		return false, err
	}
	return exists, nil
}

// DataTableExists checks if a table exists in the database
func (db *DB) DataTableExists(suffix string) (bool, error) {
	tableName := fmt.Sprintf("t_logs_data_%s", suffix)
	var exists bool
	err := db.rdb.QueryRow(db.ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)", tableName).Scan(&exists)
	if err != nil {
		// A database error occurred
		return false, err
	}
	return exists, nil
}

// TableNameSuffix returns the name of the transfer db for the given contract
func (d *DB) TableNameSuffix(contract string) (string, error) {
	re := regexp.MustCompile("^0x[0-9a-fA-F]{40}$")

	suffix := fmt.Sprintf("%v_%s", d.chainID, strings.ToLower(contract))

	if !re.MatchString(contract) {
		return suffix, errors.New("bad contract address")
	}

	return suffix, nil
}

// Close closes the db and all its transfer and push dbs
func (d *DB) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.db.Close()
	d.rdb.Close()
}
