package nostr

import (
	"context"

	"github.com/fiatjaf/eventstore/postgresql"
	"github.com/nbd-wtf/go-nostr"
)

type Nostr struct {
	secretKey string
	ndb       *postgresql.PostgresBackend

	RelayUrl string
}

func NewNostr(secretKey string,
	ndb *postgresql.PostgresBackend,
	relayUrl string) *Nostr {
	return &Nostr{
		secretKey: secretKey,
		ndb:       ndb,
		RelayUrl:  relayUrl,
	}
}

func (n *Nostr) SignAndSaveEvent(ctx context.Context, ev *nostr.Event) (*nostr.Event, error) {
	err := ev.Sign(n.secretKey)
	if err != nil {
		return nil, err
	}

	err = n.ndb.SaveEvent(ctx, ev)
	if err != nil {
		return nil, err
	}

	return ev, nil
}
