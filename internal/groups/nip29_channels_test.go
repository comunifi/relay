package groups

import (
	"context"
	"testing"
	"time"

	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/khatru"
	"github.com/nbd-wtf/go-nostr"
)

// memoryStore is a simple in-memory implementation of eventstore.Store for testing.
type memoryStore struct {
	events map[string]*nostr.Event
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		events: make(map[string]*nostr.Event),
	}
}

func (m *memoryStore) Init() error {
	return nil
}

func (m *memoryStore) Close() {}

func (m *memoryStore) QueryEvents(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	ch := make(chan *nostr.Event, len(m.events))

	go func() {
		defer close(ch)

		for _, ev := range m.events {
			if !matchesFilter(ev, filter) {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case ch <- ev:
			}
		}
	}()

	return ch, nil
}

func (m *memoryStore) DeleteEvent(ctx context.Context, ev *nostr.Event) error {
	delete(m.events, ev.ID)
	return nil
}

func (m *memoryStore) SaveEvent(ctx context.Context, ev *nostr.Event) error {
	m.events[ev.ID] = ev
	return nil
}

func (m *memoryStore) ReplaceEvent(ctx context.Context, ev *nostr.Event) error {
	// Implement simple replace semantics based on NIP-33-style \"d\" tag:
	// for the same (kind, author, d) we keep only the newest event.
	var dVal string
	for _, tag := range ev.Tags {
		if len(tag) >= 2 && tag[0] == "d" {
			dVal = tag[1]
			break
		}
	}

	if dVal != "" {
		for id, existing := range m.events {
			if existing.Kind != ev.Kind || existing.PubKey != ev.PubKey {
				continue
			}
			var existingD string
			for _, t := range existing.Tags {
				if len(t) >= 2 && t[0] == "d" {
					existingD = t[1]
					break
				}
			}
			if existingD == dVal {
				delete(m.events, id)
			}
		}
	}

	m.events[ev.ID] = ev
	return nil
}

// matchesFilter applies a very small subset of nostr.Filter logic, enough for these tests.
func matchesFilter(ev *nostr.Event, f nostr.Filter) bool {
	// Kinds
	if len(f.Kinds) > 0 {
		match := false
		for _, k := range f.Kinds {
			if ev.Kind == k {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}

	// Authors
	if len(f.Authors) > 0 {
		match := false
		for _, a := range f.Authors {
			if ev.PubKey == a {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}

	// IDs
	if len(f.IDs) > 0 {
		match := false
		for _, id := range f.IDs {
			if ev.ID == id {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}

	// Tags
	if len(f.Tags) > 0 {
		for key, values := range f.Tags {
			tagMatch := false
			for _, tag := range ev.Tags {
				if len(tag) >= 2 && tag[0] == key {
					for _, v := range values {
						if tag[1] == v {
							tagMatch = true
							break
						}
					}
				}
				if tagMatch {
					break
				}
			}
			if !tagMatch {
				return false
			}
		}
	}

	return true
}

var _ eventstore.Store = (*memoryStore)(nil)

// setupTestGroupsService creates a GroupsService with an in-memory store and a test relay keypair.
func setupTestGroupsService(t *testing.T) (*GroupsService, *memoryStore) {
	t.Helper()

	store := newMemoryStore()

	// Create a khatru.Relay with memory store hooks for testing
	relay := khatru.NewRelay()
	relay.StoreEvent = append(relay.StoreEvent, store.SaveEvent)
	relay.ReplaceEvent = append(relay.ReplaceEvent, store.ReplaceEvent)

	secret := nostr.GeneratePrivateKey()
	if secret == "" {
		t.Fatalf("failed to generate private key")
	}

	pub, err := nostr.GetPublicKey(secret)
	if err != nil {
		t.Fatalf("failed to derive public key: %v", err)
	}

	service := NewGroupsService(store, relay, pub, secret)
	return service, store
}

// createTestGroup seeds a group by simulating a KindCreateGroup event.
func createTestGroup(t *testing.T, g *GroupsService, store *memoryStore, groupID, creatorPub string) {
	t.Helper()

	ctx := context.Background()

	ev := &nostr.Event{
		ID:        "group-create-" + groupID,
		Kind:      KindCreateGroup,
		PubKey:    creatorPub,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"h", groupID},
		},
	}

	// Save and trigger metadata generation
	if err := store.SaveEvent(ctx, ev); err != nil {
		t.Fatalf("failed to save group create event: %v", err)
	}
	g.OnEventSaved(ctx, ev)
}

// addTestMember adds a member to a group by simulating a KindPutUser event and calling OnEventSaved.
func addTestMember(t *testing.T, g *GroupsService, store *memoryStore, groupID, memberPub string) {
	t.Helper()

	ctx := context.Background()

	ev := &nostr.Event{
		ID:        "put-user-" + memberPub[:8],
		Kind:      KindPutUser,
		PubKey:    g.relayPubkey, // author doesn't matter for this test
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"h", groupID},
			{"p", memberPub, RoleMember},
		},
	}

	if err := store.SaveEvent(ctx, ev); err != nil {
		t.Fatalf("failed to save put-user event: %v", err)
	}
	g.OnEventSaved(ctx, ev)
}

func TestValidateChannelCreate_RejectsNonMember(t *testing.T) {
	ctx := context.Background()

	g, store := setupTestGroupsService(t)

	// Create a group with one admin/creator
	creatorPub, err := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	if err != nil {
		t.Fatalf("failed to derive creator pubkey: %v", err)
	}
	groupID := "test-group"

	createTestGroup(t, g, store, groupID, creatorPub)

	// Non-member tries to create a channel
	nonMemberPub, err := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	if err != nil {
		t.Fatalf("failed to derive non-member pubkey: %v", err)
	}

	channelCreate := &nostr.Event{
		ID:        "chan-1",
		Kind:      KindChannelCreate,
		PubKey:    nonMemberPub,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"h", groupID},
		},
		Content: `{"name":"General"}`,
	}

	reject, _ := g.ValidateEvent(ctx, channelCreate)
	if !reject {
		t.Fatalf("expected channel creation by non-member to be rejected")
	}
}

func TestValidateChannelCreate_AllowsMember(t *testing.T) {
	ctx := context.Background()

	g, store := setupTestGroupsService(t)

	// Create group and add a member
	creatorPub, err := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	if err != nil {
		t.Fatalf("failed to derive creator pubkey: %v", err)
	}
	groupID := "test-group-2"

	createTestGroup(t, g, store, groupID, creatorPub)

	memberPub, err := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	if err != nil {
		t.Fatalf("failed to derive member pubkey: %v", err)
	}

	addTestMember(t, g, store, groupID, memberPub)

	channelCreate := &nostr.Event{
		ID:        "chan-2",
		Kind:      KindChannelCreate,
		PubKey:    memberPub,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"h", groupID},
		},
		Content: `{"name":"General"}`,
	}

	reject, msg := g.ValidateEvent(ctx, channelCreate)
	if reject {
		t.Fatalf("expected channel creation by member to be accepted, got rejection: %s", msg)
	}
}

func TestValidateChannelMetadata_RequiresETag(t *testing.T) {
	ctx := context.Background()

	g, store := setupTestGroupsService(t)

	// Create group and add a member
	creatorPub, err := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	if err != nil {
		t.Fatalf("failed to derive creator pubkey: %v", err)
	}
	groupID := "test-group-3"

	createTestGroup(t, g, store, groupID, creatorPub)

	memberPub, err := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	if err != nil {
		t.Fatalf("failed to derive member pubkey: %v", err)
	}

	addTestMember(t, g, store, groupID, memberPub)

	// Channel metadata without e tag should be rejected
	metaEvent := &nostr.Event{
		ID:        "meta-1",
		Kind:      KindChannelMetadata,
		PubKey:    memberPub,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"h", groupID},
		},
		Content: `{"name":"Updated"}`,
	}

	reject, _ := g.ValidateEvent(ctx, metaEvent)
	if !reject {
		t.Fatalf("expected channel metadata without e tag to be rejected")
	}
}

func TestGroupChannelsMetadataUpdatedOnChannelEvents(t *testing.T) {
	ctx := context.Background()

	g, store := setupTestGroupsService(t)

	// Create group and add a member
	creatorPub, err := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	if err != nil {
		t.Fatalf("failed to derive creator pubkey: %v", err)
	}
	groupID := "test-group-4"

	createTestGroup(t, g, store, groupID, creatorPub)

	memberPub, err := nostr.GetPublicKey(nostr.GeneratePrivateKey())
	if err != nil {
		t.Fatalf("failed to derive member pubkey: %v", err)
	}

	addTestMember(t, g, store, groupID, memberPub)

	// Member creates a channel
	channelCreate := &nostr.Event{
		ID:        "chan-3",
		Kind:      KindChannelCreate,
		PubKey:    memberPub,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"h", groupID},
		},
		Content: `{"name":"General","about":"General discussion"}`,
	}

	if err := store.SaveEvent(ctx, channelCreate); err != nil {
		t.Fatalf("failed to save channel create event: %v", err)
	}
	g.OnEventSaved(ctx, channelCreate)

	// After creation, there should be a group channels metadata snapshot
	meta, err := g.GetGroupChannels(ctx, groupID)
	if err != nil {
		t.Fatalf("failed to get group channels metadata: %v", err)
	}
	if len(meta.Channels) != 1 {
		t.Fatalf("expected 1 channel in metadata, got %d", len(meta.Channels))
	}
	if meta.Channels[0].Name != "General" {
		t.Fatalf("expected channel name 'General', got %q", meta.Channels[0].Name)
	}

	// Now send a metadata update for the channel
	channelMeta := &nostr.Event{
		ID:        "meta-chan-3",
		Kind:      KindChannelMetadata,
		PubKey:    memberPub,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"h", groupID},
			{"e", channelCreate.ID},
		},
		Content: `{"name":"General Updated","about":"New description"}`,
	}

	if err := store.SaveEvent(ctx, channelMeta); err != nil {
		t.Fatalf("failed to save channel metadata event: %v", err)
	}
	g.OnEventSaved(ctx, channelMeta)

	// Metadata snapshot should now reflect the updated name/about
	meta, err = g.GetGroupChannels(ctx, groupID)
	if err != nil {
		t.Fatalf("failed to get group channels metadata after update: %v", err)
	}
	if len(meta.Channels) != 1 {
		t.Fatalf("expected 1 channel in metadata after update, got %d", len(meta.Channels))
	}
	if meta.Channels[0].Name != "General Updated" {
		t.Fatalf("expected channel name 'General Updated', got %q", meta.Channels[0].Name)
	}
	if meta.Channels[0].About != "New description" {
		t.Fatalf("expected channel about 'New description', got %q", meta.Channels[0].About)
	}
}

