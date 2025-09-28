package db

import (
	"context"
	"time"

	"github.com/comunifi/relay/pkg/relay"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EventDB struct {
	ctx context.Context
	db  *pgxpool.Pool
	rdb *pgxpool.Pool
}

// NewEventDB creates a new DB
func NewEventDB(ctx context.Context, db, rdb *pgxpool.Pool) (*EventDB, error) {
	evdb := &EventDB{
		ctx: ctx,
		db:  db,
		rdb: rdb,
	}

	return evdb, nil
}

// createEventsTable creates a table to store events in the given db
func (db *EventDB) CreateEventsTable() error {
	_, err := db.db.Exec(db.ctx, `
	CREATE TABLE IF NOT EXISTS t_events(
		chain_id text NOT NULL,
		contract text NOT NULL,
		topic text NOT NULL,
		alias text NOT NULL,
		event_signature text NOT NULL,
		name text NOT NULL,
		created_at timestamp NOT NULL DEFAULT current_timestamp,
		updated_at timestamp NOT NULL DEFAULT current_timestamp,
		PRIMARY KEY (chain_id, contract, topic)
	);
	`)

	return err
}

// createEventsTableIndexes creates the indexes for events in the given db
func (db *EventDB) CreateEventsTableIndexes() error {
	_, err := db.db.Exec(db.ctx, `
    CREATE INDEX IF NOT EXISTS idx_events_contract ON t_events (chain_id, contract);
    `)
	if err != nil {
		return err
	}

	_, err = db.db.Exec(db.ctx, `
    CREATE INDEX IF NOT EXISTS idx_events_contract_signature ON t_events (chain_id, contract, topic);
    `)
	if err != nil {
		return err
	}

	return nil
}

// EventExists checks if an event exists in the db
func (db *EventDB) EventExists(chainID string, contract string) (bool, error) {
	var exists bool
	err := db.rdb.QueryRow(db.ctx, `
	SELECT EXISTS (SELECT 1 FROM t_events WHERE chain_id = $1 AND contract = $2)
	`, chainID, contract).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// GetEvent gets an event from the db by contract and signature
func (db *EventDB) GetEvent(chainID string, contract string, topic string) (*relay.Event, error) {
	var event relay.Event
	err := db.rdb.QueryRow(db.ctx, `
	SELECT chain_id, contract, topic, alias, event_signature, name, created_at, updated_at
	FROM t_events
	WHERE chain_id = $1 AND contract = $2 AND topic = $3
	`, chainID, contract, topic).Scan(&event.ChainID, &event.Contract, &event.Topic, &event.Alias, &event.EventSignature, &event.Name, &event.CreatedAt, &event.UpdatedAt)
	if err != nil {
		return nil, err
	}

	return &event, nil
}

// GetEvents gets all events from the db
func (db *EventDB) GetEvents(chainID string) ([]*relay.Event, error) {
	rows, err := db.rdb.Query(db.ctx, `
    SELECT chain_id, contract, topic, alias, event_signature, name, created_at, updated_at
    FROM t_events
	WHERE chain_id = $1
    ORDER BY created_at ASC
    `, chainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []*relay.Event{}
	for rows.Next() {
		var event relay.Event
		err = rows.Scan(&event.ChainID, &event.Contract, &event.Topic, &event.Alias, &event.EventSignature, &event.Name, &event.CreatedAt, &event.UpdatedAt)
		if err != nil {
			return nil, err
		}

		events = append(events, &event)
	}

	return events, nil
}

// GetOutdatedEvents gets all queued events from the db sorted by created_at
func (db *EventDB) GetOutdatedEvents(chainID string, currentBlk int64) ([]*relay.Event, error) {
	rows, err := db.rdb.Query(db.ctx, `
    SELECT chain_id, contract, topic, alias, event_signature, name, created_at, updated_at
    FROM t_events
    WHERE chain_id = $1 AND last_block < $2
    ORDER BY created_at ASC
    `, chainID, currentBlk)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []*relay.Event{}
	for rows.Next() {
		var event relay.Event
		err = rows.Scan(&event.ChainID, &event.Contract, &event.Topic, &event.Alias, &event.EventSignature, &event.Name, &event.CreatedAt, &event.UpdatedAt)
		if err != nil {
			return nil, err
		}

		events = append(events, &event)
	}

	return events, nil
}

// SetEventLastBlock sets the last block of an event
func (db *EventDB) SetEventLastBlock(chainID string, contract string, topic string, lastBlock int64) error {
	_, err := db.db.Exec(db.ctx, `
    UPDATE t_events
    SET last_block = $1, updated_at = $2
    WHERE chain_id = $3 AND contract = $4 AND topic = $5
    `, lastBlock, time.Now().UTC(), chainID, contract, topic)

	return err
}

// AddEvent adds an event to the db
func (db *EventDB) AddEvent(chainID string, contract string, topic string, alias string, signature string, name string) error {
	t := time.Now().UTC()

	_, err := db.db.Exec(db.ctx, `
    INSERT INTO t_events (chain_id, contract, topic, alias, event_signature, name, created_at, updated_at)
    VALUES ($1, $2, $3, $4, $5, $6, $7)
    ON CONFLICT (chain_id, contract, topic)
    DO UPDATE SET
        name = EXCLUDED.name,
        updated_at = EXCLUDED.updated_at
    `, chainID, contract, topic, alias, signature, name, t, t)
	if err != nil {
		return err
	}

	return err
}
