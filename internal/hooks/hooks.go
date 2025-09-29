package hooks

import (
	"math/big"

	"github.com/comunifi/relay/internal/db"
	"github.com/comunifi/relay/internal/nostr"
	"github.com/comunifi/relay/internal/queue"
	"github.com/comunifi/relay/internal/userop"
	"github.com/comunifi/relay/pkg/relay"
	"github.com/fiatjaf/eventstore/postgresql"
	"github.com/fiatjaf/khatru"
)

type Router struct {
	evm     relay.EVMRequester
	db      *db.DB
	n       *nostr.Nostr
	useropq *queue.Service
	chainID *big.Int
	ndb     *postgresql.PostgresBackend
}

func NewRouter(evm relay.EVMRequester, db *db.DB, n *nostr.Nostr, useropq *queue.Service, chainID *big.Int, ndb *postgresql.PostgresBackend) *Router {
	return &Router{evm: evm, db: db, n: n, useropq: useropq, chainID: chainID, ndb: ndb}
}

func (r *Router) AddHooks(relay *khatru.Relay) *khatru.Relay {
	// instantiate handlers
	uop := userop.NewService(r.evm, r.db, r.n, r.useropq, r.chainID)

	// saving events
	relay.StoreEvent = append(relay.StoreEvent, r.ndb.SaveEvent)
	relay.StoreEvent = append(relay.StoreEvent, uop.Process)

	// querying events
	relay.QueryEvents = append(relay.QueryEvents, r.ndb.QueryEvents)

	// counting events
	relay.CountEvents = append(relay.CountEvents, r.ndb.CountEvents)

	// deleting events
	relay.DeleteEvent = append(relay.DeleteEvent, r.ndb.DeleteEvent)

	// replacing events
	relay.ReplaceEvent = append(relay.ReplaceEvent, r.ndb.ReplaceEvent)

	return relay
}
