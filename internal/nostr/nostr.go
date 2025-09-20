package nostr

import (
	"context"

	"github.com/fiatjaf/eventstore/postgresql"
	"github.com/nbd-wtf/go-nostr"
)

type Nostr struct {
	secretKey string
	ndb       *postgresql.PostgresBackend
}

func NewNostr(secretKey string,
	ndb *postgresql.PostgresBackend) *Nostr {
	return &Nostr{
		secretKey: secretKey,
		ndb:       ndb,
	}
}

func (n *Nostr) SignAndSaveEvent(ctx context.Context, ev *nostr.Event) error {
	err := ev.Sign(n.secretKey)
	if err != nil {
		return err
	}

	return n.ndb.SaveEvent(ctx, ev)
}
