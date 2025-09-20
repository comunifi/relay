package db

import (
	"context"
	"fmt"

	"github.com/comunifi/relay/pkg/common"
	"github.com/comunifi/relay/pkg/relay"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SponsorDB struct {
	ctx    context.Context
	suffix string
	secret string
	db     *pgxpool.Pool
	rdb    *pgxpool.Pool
}

// NewSponsorDB creates a new DB
func NewSponsorDB(ctx context.Context, db, rdb *pgxpool.Pool, name, secret string) (*SponsorDB, error) {

	sdb := &SponsorDB{
		ctx:    ctx,
		suffix: name,
		secret: secret,
		db:     db,
		rdb:    rdb,
	}

	return sdb, nil
}

// createSponsorsTable creates a table to store sponsors in the given db
func (db *SponsorDB) CreateSponsorsTable(suffix string) error {
	_, err := db.db.Exec(db.ctx, fmt.Sprintf(`
	CREATE TABLE t_sponsors_%s(
		contract TEXT NOT NULL PRIMARY KEY,
		pk text NOT NULL,
		created_at timestamp NOT NULL DEFAULT current_timestamp,
		updated_at timestamp NOT NULL DEFAULT current_timestamp
	);
	`, suffix))

	return err
}

// createSponsorsTableIndexes creates the indexes for sponsors in the given db
func (db *SponsorDB) CreateSponsorsTableIndexes(suffix string) error {
	return nil
}

// GetSponsor gets a sponsor from the db by contract
func (db *SponsorDB) GetSponsor(contract string) (*relay.Sponsor, error) {
	var sponsor relay.Sponsor
	err := db.rdb.QueryRow(db.ctx, fmt.Sprintf(`
	SELECT contract, pk, created_at, updated_at
	FROM t_sponsors_%s
	WHERE contract = $1
	`, db.suffix), contract).Scan(&sponsor.Contract, &sponsor.PrivateKey, &sponsor.CreatedAt, &sponsor.UpdatedAt)
	if err != nil {
		return nil, err
	}

	decrypted, err := common.Decrypt(sponsor.PrivateKey, db.secret)
	if err != nil {
		return nil, err
	}

	sponsor.PrivateKey = decrypted

	return &sponsor, nil
}

// AddSponsor adds a sponsor to the db
func (db *SponsorDB) AddSponsor(sponsor *relay.Sponsor) error {
	encrypted, err := common.Encrypt(sponsor.PrivateKey, db.secret)
	if err != nil {
		return err
	}

	_, err = db.db.Exec(db.ctx, fmt.Sprintf(`
	INSERT INTO t_sponsors_%s(contract, pk, created_at, updated_at)
	VALUES($1, $2, $3, $4)
	`, db.suffix), sponsor.Contract, encrypted, sponsor.CreatedAt, sponsor.UpdatedAt)
	if err != nil {
		return err
	}

	return nil
}

// UpdateSponsor updates a sponsor in the db
func (db *SponsorDB) UpdateSponsor(sponsor *relay.Sponsor) error {
	encrypted, err := common.Encrypt(sponsor.PrivateKey, db.secret)
	if err != nil {
		return err
	}

	_, err = db.db.Exec(db.ctx, fmt.Sprintf(`
	UPDATE t_sponsors_%s
	SET pk = $1, updated_at = $2
	WHERE contract = $3
	`, db.suffix), encrypted, sponsor.UpdatedAt, sponsor.Contract)
	if err != nil {
		return err
	}

	return nil
}
