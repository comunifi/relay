package nostr

import (
	"strconv"

	nostreth "github.com/comunifi/nostr-eth"
	"github.com/lib/pq"
	"github.com/nbd-wtf/go-nostr"
)

// GetEvent returns the event for a given id
func (n *Nostr) GetEvent(id string) (*nostr.Event, error) {
	// Query the event table for events where the "t" tag matches the chain ID and "d" tag matches the hash
	row := n.ndb.QueryRow(`
		SELECT id, pubkey, created_at, kind, content, sig, tags
		FROM event
		WHERE id = $1
	`, 1, id)

	var event nostr.Event

	err := row.Scan(&event.ID, &event.PubKey, &event.CreatedAt, &event.Kind, &event.Content, &event.Sig, &event.Tags)
	if err != nil {
		return nil, err
	}

	return &event, nil
}

// GetMentionEvent returns the mention event for a given id
func (n *Nostr) GetMentionEvent(id string) (*nostr.Event, error) {
	// Collect unique values for tagvalues query
	tagValues := []string{id, strconv.Itoa(nostreth.KindTxTransfer)}

	// Query the event table for mention events that reference the given event ID
	row := n.ndb.QueryRow(`
		SELECT id, pubkey, created_at, kind, content, sig, tags
		FROM event
		WHERE kind = 1
		AND tagvalues @> $1
		LIMIT 1
	`, pq.Array(tagValues))

	var event nostr.Event

	err := row.Scan(&event.ID, &event.PubKey, &event.CreatedAt, &event.Kind, &event.Content, &event.Sig, &event.Tags)
	if err != nil {
		return nil, err
	}

	event.Content = removeNostrUris(event.Content)

	return &event, nil
}
