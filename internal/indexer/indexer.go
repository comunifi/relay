package indexer

import (
	"context"
	"errors"
	"math/big"

	"github.com/comunifi/relay/internal/db"
	"github.com/comunifi/relay/internal/ws"
	"github.com/comunifi/relay/pkg/relay"
	"github.com/fiatjaf/eventstore/postgresql"
)

type ErrIndexing error

var (
	ErrIndexingRecoverable ErrIndexing = errors.New("error indexing recoverable") // an error occurred while indexing but it is not fatal
)

type Indexer struct {
	ctx       context.Context
	secretKey string
	chainID   *big.Int

	db  *db.DB
	ndb *postgresql.PostgresBackend
	evm relay.EVMRequester

	pools *ws.ConnectionPools
}

func NewIndexer(ctx context.Context, secretKey string, chainID *big.Int, db *db.DB, ndb *postgresql.PostgresBackend, evm relay.EVMRequester, pools *ws.ConnectionPools) *Indexer {
	return &Indexer{ctx: ctx, secretKey: secretKey, chainID: chainID, db: db, ndb: ndb, evm: evm, pools: pools}
}

func (i *Indexer) Start() error {
	evs, err := i.db.EventDB.GetEvents()
	if err != nil {
		return err
	}

	quitAck := make(chan error)

	for _, ev := range evs {
		go func() {
			err := i.ListenToLogs(ev, quitAck)
			if err != nil {
				quitAck <- err
			}
		}()
	}

	return <-quitAck
}
