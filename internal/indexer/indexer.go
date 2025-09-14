package indexer

import (
	"context"
	"errors"

	"github.com/citizenapp2/relay/internal/db"
	"github.com/citizenapp2/relay/internal/ws"
	"github.com/citizenapp2/relay/pkg/relay"
)

type ErrIndexing error

var (
	ErrIndexingRecoverable ErrIndexing = errors.New("error indexing recoverable") // an error occurred while indexing but it is not fatal
)

type Indexer struct {
	ctx context.Context
	db  *db.DB
	evm relay.EVMRequester

	pools *ws.ConnectionPools
}

func NewIndexer(ctx context.Context, db *db.DB, evm relay.EVMRequester, pools *ws.ConnectionPools) *Indexer {
	return &Indexer{ctx: ctx, db: db, evm: evm, pools: pools}
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
