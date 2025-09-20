package logdb

import (
	"context"
	"fmt"
	"time"

	"github.com/comunifi/relay/pkg/relay"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EventDB struct {
	ctx    context.Context
	suffix string
	db     *pgxpool.Pool
	rdb    *pgxpool.Pool
}

// NewEventDB creates a new DB
func NewEventDB(ctx context.Context, db, rdb *pgxpool.Pool, name string) (*EventDB, error) {
	evdb := &EventDB{
		ctx:    ctx,
		suffix: name,
		db:     db,
		rdb:    rdb,
	}

	return evdb, nil
}

// createEventsTable creates a table to store events in the given db
func (db *EventDB) CreateEventsTable(suffix string) error {
	_, err := db.db.Exec(db.ctx, fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS t_events_%s(
		contract text NOT NULL,
		event_signature text NOT NULL,
		name text NOT NULL,
		created_at timestamp NOT NULL DEFAULT current_timestamp,
		updated_at timestamp NOT NULL DEFAULT current_timestamp,
		UNIQUE (contract, event_signature)
	);
	`, suffix))

	return err
}

// createEventsTableIndexes creates the indexes for events in the given db
func (db *EventDB) CreateEventsTableIndexes(suffix string) error {
	_, err := db.db.Exec(db.ctx, fmt.Sprintf(`
    CREATE INDEX IF NOT EXISTS idx_events_%s_contract ON t_events_%s (contract);
    `, suffix, suffix))
	if err != nil {
		return err
	}

	_, err = db.db.Exec(db.ctx, fmt.Sprintf(`
    CREATE INDEX IF NOT EXISTS idx_events_%s_contract_signature ON t_events_%s (contract, event_signature);
    `, suffix, suffix))
	if err != nil {
		return err
	}

	return nil
}

// EventExists checks if an event exists in the db
func (db *EventDB) EventExists(contract string) (bool, error) {
	var exists bool
	err := db.rdb.QueryRow(db.ctx, fmt.Sprintf(`
	SELECT EXISTS (SELECT 1 FROM t_events_%s WHERE contract = $1)
	`, db.suffix), contract).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// GetEvent gets an event from the db by contract and signature
func (db *EventDB) GetEvent(contract string, signature string) (*relay.Event, error) {
	var event relay.Event
	err := db.rdb.QueryRow(db.ctx, fmt.Sprintf(`
	SELECT contract, event_signature, name, created_at, updated_at
	FROM t_events_%s
	WHERE contract = $1 AND event_signature = $2
	`, db.suffix), contract, signature).Scan(&event.Contract, &event.EventSignature, &event.Name, &event.CreatedAt, &event.UpdatedAt)
	if err != nil {
		return nil, err
	}

	return &event, nil
}

// GetEvents gets all events from the db
func (db *EventDB) GetEvents() ([]*relay.Event, error) {
	rows, err := db.rdb.Query(db.ctx, fmt.Sprintf(`
    SELECT contract, event_signature, name, created_at, updated_at
    FROM t_events_%s
    ORDER BY created_at ASC
    `, db.suffix))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []*relay.Event{}
	for rows.Next() {
		var event relay.Event
		err = rows.Scan(&event.Contract, &event.EventSignature, &event.Name, &event.CreatedAt, &event.UpdatedAt)
		if err != nil {
			return nil, err
		}

		events = append(events, &event)
	}

	return events, nil
}

// GetOutdatedEvents gets all queued events from the db sorted by created_at
func (db *EventDB) GetOutdatedEvents(currentBlk int64) ([]*relay.Event, error) {
	rows, err := db.rdb.Query(db.ctx, fmt.Sprintf(`
    SELECT contract, event_signature, name, created_at, updated_at
    FROM t_events_%s
    WHERE last_block < $1
    ORDER BY created_at ASC
    `, db.suffix), currentBlk)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []*relay.Event{}
	for rows.Next() {
		var event relay.Event
		err = rows.Scan(&event.Contract, &event.EventSignature, &event.Name, &event.CreatedAt, &event.UpdatedAt)
		if err != nil {
			return nil, err
		}

		events = append(events, &event)
	}

	return events, nil
}

// SetEventLastBlock sets the last block of an event
func (db *EventDB) SetEventLastBlock(contract string, signature string, lastBlock int64) error {
	_, err := db.db.Exec(db.ctx, fmt.Sprintf(`
    UPDATE t_events_%s
    SET last_block = $1, updated_at = $2
    WHERE contract = $3 AND event_signature = $4
    `, db.suffix), lastBlock, time.Now().UTC(), contract, signature)

	return err
}

// AddEvent adds an event to the db
func (db *EventDB) AddEvent(contract string, signature string, name string) error {
	t := time.Now().UTC()

	_, err := db.db.Exec(db.ctx, fmt.Sprintf(`
    INSERT INTO t_events_%s (contract, event_signature, name, created_at, updated_at)
    VALUES ($1, $2, $3, $4, $5)
    ON CONFLICT (contract, event_signature)
    DO UPDATE SET
        name = EXCLUDED.name,
        updated_at = EXCLUDED.updated_at
    `, db.suffix), contract, signature, name, t, t)
	if err != nil {
		return err
	}

	return err
}
