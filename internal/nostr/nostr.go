package nostr

import (
	"context"
	"fmt"

	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/eventstore/postgresql"
	"github.com/fiatjaf/khatru"
	"github.com/nbd-wtf/go-nostr"
)

type Nostr struct {
	secretKey string
	ndb       *postgresql.PostgresBackend
	kh        *khatru.Relay

	RelayUrl string
}

func NewNostr(secretKey string,
	ndb *postgresql.PostgresBackend,
	kh *khatru.Relay,
	relayUrl string) *Nostr {
	return &Nostr{
		secretKey: secretKey,
		ndb:       ndb,
		kh:        kh,
		RelayUrl:  relayUrl,
	}
}

func (n *Nostr) SignAndSaveEvent(ctx context.Context, ev *nostr.Event) (*nostr.Event, error) {
	err := ev.Sign(n.secretKey)
	if err != nil {
		return nil, err
	}

	for _, store := range n.kh.StoreEvent {
		err := store(ctx, ev)
		if err != nil {
			return nil, err
		}
	}

	return ev, nil
}

func (n *Nostr) SignAndReplaceEvent(ctx context.Context, ev *nostr.Event) (*nostr.Event, error) {
	err := ev.Sign(n.secretKey)
	if err != nil {
		return nil, err
	}

	filter := nostr.Filter{Limit: 1, Kinds: []int{ev.Kind}, Authors: []string{ev.PubKey}}
	filter.Tags = nostr.TagMap{"d": []string{ev.Tags.GetD()}}

	ch, err := n.ndb.QueryEvents(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to query before replacing: %w", err)
	}

	shouldStore := true
	for previous := range ch {
		if IsOlder(previous, ev) {
			if err := n.ndb.DeleteEvent(ctx, previous); err != nil {
				return nil, fmt.Errorf("failed to delete event for replacing: %w", err)
			}
		} else {
			shouldStore = false
		}
	}

	if shouldStore {
		if err := n.ndb.SaveEvent(ctx, ev); err != nil && err != eventstore.ErrDupEvent {
			return nil, fmt.Errorf("failed to save: %w", err)
		}
	}

	return ev, nil
}

func IsOlder(previous, next *nostr.Event) bool {
	return previous.CreatedAt < next.CreatedAt ||
		(previous.CreatedAt == next.CreatedAt && previous.ID > next.ID)
}
