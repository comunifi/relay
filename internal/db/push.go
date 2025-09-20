package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/comunifi/relay/pkg/common"
	"github.com/comunifi/relay/pkg/relay"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PushTokenDB struct {
	ctx    context.Context
	suffix string
	db     *pgxpool.Pool
	rdb    *pgxpool.Pool
}

// NewPushTokenDB creates a new DB
func NewPushTokenDB(ctx context.Context, db, rdb *pgxpool.Pool, name string) (*PushTokenDB, error) {
	txdb := &PushTokenDB{
		ctx:    ctx,
		suffix: name,
		db:     db,
		rdb:    rdb,
	}

	return txdb, nil
}

// CreatePushTable creates a table to store push tokens in the given db
func (db *PushTokenDB) CreatePushTable() error {
	_, err := db.db.Exec(db.ctx, fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS t_push_token_%s(
		token TEXT NOT NULL,
		account text NOT NULL,
		created_at timestamp NOT NULL DEFAULT current_timestamp,
		updated_at timestamp NOT NULL DEFAULT current_timestamp,
		UNIQUE (token, account)
	);
	`, db.suffix))

	return err
}

// CreatePushTableIndexes creates the indexes for push in the given db
func (db *PushTokenDB) CreatePushTableIndexes() error {
	suffix := common.ShortenName(db.suffix, 6)

	// fetch tokens for an address
	_, err := db.db.Exec(db.ctx, fmt.Sprintf(`
	CREATE INDEX IF NOT EXISTS idx_push_%s_account ON t_push_token_%s (account);
	`, suffix, db.suffix))
	if err != nil {
		return err
	}

	_, err = db.db.Exec(db.ctx, fmt.Sprintf(`
	CREATE INDEX IF NOT EXISTS idx_push_%s_token_account ON t_push_token_%s (token, account);
	`, suffix, db.suffix))
	if err != nil {
		return err
	}

	return nil
}

// AddToken adds a token to the db
func (db *PushTokenDB) AddToken(p *relay.PushToken) error {
	now := time.Now().UTC()

	_, err := db.db.Exec(db.ctx, fmt.Sprintf(`
	INSERT INTO t_push_token_%s (token, account, created_at, updated_at)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (token, account)
	DO UPDATE SET updated_at = $4
	`, db.suffix), p.Token, p.Account, now, now)
	if err != nil {
		return err
	}

	return nil
}

// GetAccountTokens returns the push tokens for a given account
func (db *PushTokenDB) GetAccountTokens(account string) ([]*relay.PushToken, error) {
	pt := []*relay.PushToken{}

	rows, err := db.rdb.Query(db.ctx, fmt.Sprintf(`
		SELECT token, account
		FROM t_push_token_%s
		WHERE account = $1
		`, db.suffix), account)
	if err != nil {
		if err == sql.ErrNoRows {
			return pt, nil
		}

		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var p relay.PushToken

		err := rows.Scan(&p.Token, &p.Account)
		if err != nil {
			return nil, err
		}

		pt = append(pt, &p)
	}

	return pt, nil
}

// RemoveAccountPushToken removes a push token for a given account from the db
func (db *PushTokenDB) RemoveAccountPushToken(token, account string) error {
	_, err := db.db.Exec(db.ctx, fmt.Sprintf(`
	DELETE FROM t_push_token_%s WHERE token = $1 AND account = $2
	`, db.suffix), token, account)

	return err
}

// RemovePushToken removes a push token from the db
func (db *PushTokenDB) RemovePushToken(token string) error {
	_, err := db.db.Exec(db.ctx, fmt.Sprintf(`
	DELETE FROM t_push_token_%s WHERE token = $1
	`, db.suffix), token)

	return err
}
