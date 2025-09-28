package db

import (
	"context"
	"errors"
	"fmt"
	"log"
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

	EventDB     *EventDB
	SponsorDB   *SponsorDB
	PushTokenDB map[string]*PushTokenDB
	DataDB      *DataDB
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

	eventDB, err := NewEventDB(ctx, db, db)
	if err != nil {
		return nil, err
	}

	sponsorDB, err := NewSponsorDB(ctx, db, db, evname, secret)
	if err != nil {
		return nil, err
	}

	datadb, err := NewDataDB(ctx, db, db)
	if err != nil {
		return nil, err
	}

	d := &DB{
		ctx:       ctx,
		chainID:   chainID,
		db:        db,
		rdb:       db,
		EventDB:   eventDB,
		SponsorDB: sponsorDB,
		DataDB:    datadb,
	}

	// check if db exists before opening, since we use rwc mode
	exists, err := d.EventTableExists()
	if err != nil {
		return nil, err
	}

	if !exists {
		// create table
		err = eventDB.CreateEventsTable()
		if err != nil {
			return nil, err
		}

		// create indexes
		err = eventDB.CreateEventsTableIndexes()
		if err != nil {
			return nil, err
		}
	}

	// check if db exists before opening, since we use rwc mode
	exists, err = d.SponsorTableExists(evname)
	if err != nil {
		return nil, err
	}

	if !exists {
		// create table
		err = sponsorDB.CreateSponsorsTable(evname)
		if err != nil {
			return nil, err
		}

		// create indexes
		err = sponsorDB.CreateSponsorsTableIndexes(evname)
		if err != nil {
			return nil, err
		}
	}

	log.Default().Println("creating data db")

	// check if db exists before opening, since we use rwc mode
	exists, err = d.DataTableExists()
	if err != nil {
		return nil, err
	}

	if !exists {
		// create table
		err = datadb.CreateDataTable()
		if err != nil {
			return nil, err
		}

		// create indexes
		err = datadb.CreateDataTableIndexes()
		if err != nil {
			return nil, err
		}
	}

	ptdb := map[string]*PushTokenDB{}

	evs, err := eventDB.GetEvents(chainID.String())
	if err != nil {
		return nil, err
	}

	for _, ev := range evs {
		name, err := d.TableNameSuffix(ev.Contract)
		if err != nil {
			return nil, err
		}

		log.Default().Println("creating push token db for: ", name)

		ptdb[name], err = NewPushTokenDB(ctx, db, db, name)
		if err != nil {
			return nil, err
		}

		// check if db exists before opening, since we use rwc mode
		exists, err = d.PushTokenTableExists(name)
		if err != nil {
			return nil, err
		}

		if !exists {
			// create table
			err = ptdb[name].CreatePushTable()
			if err != nil {
				return nil, err
			}

			// create indexes
			err = ptdb[name].CreatePushTableIndexes()
			if err != nil {
				return nil, err
			}
		}
	}

	d.PushTokenDB = ptdb

	return d, nil
}

// EventTableExists checks if a table exists in the database
func (db *DB) EventTableExists() (bool, error) {
	tableName := "t_events"
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
func (db *DB) DataTableExists() (bool, error) {
	tableName := "t_logs_data"
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

// GetPushTokenDB returns true if the push token db for the given contract exists, returns the db if it exists
func (d *DB) GetPushTokenDB(contract string) (*PushTokenDB, bool) {
	name, err := d.TableNameSuffix(contract)
	if err != nil {
		return nil, false
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	ptdb, ok := d.PushTokenDB[name]
	if !ok {
		return nil, false
	}
	return ptdb, true
}

// AddPushTokenDB adds a new push token db for the given contract
func (d *DB) AddPushTokenDB(contract string) (*PushTokenDB, error) {
	name, err := d.TableNameSuffix(contract)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if ptdb, ok := d.PushTokenDB[name]; ok {
		return ptdb, nil
	}
	ptdb, err := NewPushTokenDB(d.ctx, d.db, d.rdb, name)
	if err != nil {
		return nil, err
	}
	d.PushTokenDB[name] = ptdb
	return ptdb, nil
}

// Close closes the db and all its transfer and push dbs
func (d *DB) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for i := range d.PushTokenDB {
		delete(d.PushTokenDB, i)
	}

	d.db.Close()
	d.rdb.Close()
}
