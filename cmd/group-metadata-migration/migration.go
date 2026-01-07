package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/comunifi/relay/internal/groups"
	"github.com/fiatjaf/eventstore"
	"github.com/nbd-wtf/go-nostr"
)

// Migrator handles rebuilding group metadata from event history
type Migrator struct {
	eventStore     eventstore.Store
	relayPubkey    string
	relaySecretKey string
	dryRun         bool
}

// GroupState represents the computed state of a group
type GroupState struct {
	ID            string
	Owner         string            // Creator's pubkey (from 9007 event)
	PersonalOwner string            // Personal group owner pubkey (from "personal" tag)
	Tags          map[string]string // All metadata tags (name, about, picture, etc.)
	Flags         map[string]bool   // Boolean flags (closed, private, open, public, etc.)
	Admins        map[string]bool   // pubkey -> is admin
	Members       map[string]bool   // pubkey -> is member
}

// NewMigrator creates a new migrator instance
func NewMigrator(eventStore eventstore.Store, relayPubkey, relaySecretKey string, dryRun bool) *Migrator {
	return &Migrator{
		eventStore:     eventStore,
		relayPubkey:    relayPubkey,
		relaySecretKey: relaySecretKey,
		dryRun:         dryRun,
	}
}

// MigrateAllGroups finds all groups and migrates each one
func (m *Migrator) MigrateAllGroups(ctx context.Context) error {
	// Find all groups by querying create-group events
	groupIDs, err := m.findAllGroups(ctx)
	if err != nil {
		return fmt.Errorf("failed to find groups: %w", err)
	}

	log.Printf("found %d groups to migrate", len(groupIDs))

	for i, groupID := range groupIDs {
		log.Printf("[%d/%d] migrating group: %s", i+1, len(groupIDs), groupID)
		if err := m.MigrateGroup(ctx, groupID); err != nil {
			log.Printf("  ERROR: %v", err)
			// Continue with other groups
		}
	}

	return nil
}

// MigrateGroup rebuilds metadata for a single group
func (m *Migrator) MigrateGroup(ctx context.Context, groupID string) error {
	// 1. Query all moderation events for this group
	events, err := m.queryGroupEvents(ctx, groupID)
	if err != nil {
		return fmt.Errorf("failed to query group events: %w", err)
	}

	log.Printf("  found %d moderation events", len(events))

	if len(events) == 0 {
		log.Printf("  no events found, skipping")
		return nil
	}

	// 2. Replay events to compute current state
	state := m.replayEvents(groupID, events)

	log.Printf("  computed state: %d admins, %d members, %d tags, %d flags",
		len(state.Admins), len(state.Members), len(state.Tags), len(state.Flags))
	if name, ok := state.Tags["name"]; ok {
		log.Printf("  group name: %s", name)
	}

	// 3. Delete old metadata events
	if err := m.deleteOldMetadata(ctx, groupID); err != nil {
		return fmt.Errorf("failed to delete old metadata: %w", err)
	}

	// 4. Generate new metadata events
	if err := m.generateMetadataEvents(ctx, state); err != nil {
		return fmt.Errorf("failed to generate metadata: %w", err)
	}

	log.Printf("  migration complete")
	return nil
}

// findAllGroups returns all group IDs from create-group events
func (m *Migrator) findAllGroups(ctx context.Context) ([]string, error) {
	filter := nostr.Filter{
		Kinds: []int{groups.KindCreateGroup},
	}

	eventsCh, err := m.eventStore.QueryEvents(ctx, filter)
	if err != nil {
		return nil, err
	}

	groupSet := make(map[string]bool)
	for event := range eventsCh {
		groupID := getHTag(event)
		if groupID != "" {
			groupSet[groupID] = true
		}
	}

	// Convert to slice
	groupIDs := make([]string, 0, len(groupSet))
	for id := range groupSet {
		groupIDs = append(groupIDs, id)
	}

	// Sort for consistent ordering
	sort.Strings(groupIDs)

	return groupIDs, nil
}

// queryGroupEvents fetches all moderation events for a group, sorted by time
func (m *Migrator) queryGroupEvents(ctx context.Context, groupID string) ([]*nostr.Event, error) {
	filter := nostr.Filter{
		Kinds: []int{
			groups.KindCreateGroup,
			groups.KindPutUser,
			groups.KindRemoveUser,
			groups.KindEditMetadata,
			groups.KindLeaveRequest,
		},
		Tags: nostr.TagMap{"h": []string{groupID}},
	}

	eventsCh, err := m.eventStore.QueryEvents(ctx, filter)
	if err != nil {
		return nil, err
	}

	var events []*nostr.Event
	for event := range eventsCh {
		events = append(events, event)
	}

	// Sort by CreatedAt ascending (oldest first)
	sort.Slice(events, func(i, j int) bool {
		if events[i].CreatedAt == events[j].CreatedAt {
			// Tie-breaker: use event ID for deterministic ordering
			return events[i].ID < events[j].ID
		}
		return events[i].CreatedAt < events[j].CreatedAt
	})

	return events, nil
}

// replayEvents processes events in order to compute final state
func (m *Migrator) replayEvents(groupID string, events []*nostr.Event) *GroupState {
	state := &GroupState{
		ID:      groupID,
		Tags:    make(map[string]string),
		Flags:   make(map[string]bool),
		Admins:  make(map[string]bool),
		Members: make(map[string]bool),
	}

	for _, event := range events {
		switch event.Kind {
		case groups.KindCreateGroup:
			// Creator becomes admin and is the owner
			state.Owner = event.PubKey
			state.Admins[event.PubKey] = true
			// Extract initial metadata
			m.extractMetadata(state, event)

		case groups.KindPutUser:
			// Add user with specified role
			for _, tag := range event.Tags {
				if len(tag) >= 2 && tag[0] == "p" {
					pubkey := tag[1]
					role := groups.RoleMember
					if len(tag) >= 3 {
						role = tag[2]
					}

					if role == groups.RoleAdmin {
						state.Admins[pubkey] = true
					}
					state.Members[pubkey] = true
				}
			}

		case groups.KindRemoveUser:
			// Remove user from both lists
			for _, tag := range event.Tags {
				if len(tag) >= 2 && tag[0] == "p" {
					pubkey := tag[1]
					delete(state.Admins, pubkey)
					delete(state.Members, pubkey)
				}
			}

		case groups.KindEditMetadata:
			// Update metadata
			m.extractMetadata(state, event)

		case groups.KindLeaveRequest:
			// User left - remove from both lists
			delete(state.Admins, event.PubKey)
			delete(state.Members, event.PubKey)
		}
	}

	return state
}

// extractMetadata extracts all metadata tags from an event
// Tags with values (name, about, picture, etc.) go into Tags map
// Tags without values (closed, private, open, public, etc.) go into Flags map
// Special handling for "personal" tag which indicates personal group ownership
//
// Each call replaces Tags and Flags completely (except PersonalOwner which is preserved)
// This ensures that 9002 (edit-metadata) events fully replace prior metadata state
func (m *Migrator) extractMetadata(state *GroupState, event *nostr.Event) {
	// Clear existing Tags and Flags to ensure each metadata event sets the full state
	// PersonalOwner is preserved as it's only set by 9007 and shouldn't be cleared by 9002
	state.Tags = make(map[string]string)
	state.Flags = make(map[string]bool)

	// Skip tags that are not metadata
	skipTags := map[string]bool{
		"h":          true, // group ID tag
		"p":          true, // pubkey tag (we handle "personal" specially)
		"e":          true, // event reference
		"client":     true, // client info tag
		"client_sig": true, // client signature tag
	}

	for _, tag := range event.Tags {
		if len(tag) == 0 {
			continue
		}

		tagName := tag[0]

		// Special handling for "personal" tag - extract owner pubkey
		if tagName == "personal" && len(tag) >= 2 {
			state.PersonalOwner = tag[1]
			continue
		}

		if skipTags[tagName] {
			continue
		}

		if len(tag) >= 2 && tag[1] != "" {
			// Tag with value
			state.Tags[tagName] = tag[1]
		} else {
			// Flag tag (no value, like "closed", "private", "open", "public")
			state.Flags[tagName] = true
		}
	}
}

// deleteOldMetadata removes existing 39xxx events for the group from any author
func (m *Migrator) deleteOldMetadata(ctx context.Context, groupID string) error {
	if m.dryRun {
		log.Printf("  [DRY RUN] would delete old 39000, 39001, 39002 events")
		return nil
	}

	kinds := []int{groups.KindGroupMetadata, groups.KindGroupAdmins, groups.KindGroupMembers}

	for _, kind := range kinds {
		// Query both "d" tag (NIP-29 standard) and "h" tag (legacy) to catch all old events
		for _, tagKey := range []string{"d", "h"} {
			filter := nostr.Filter{
				Kinds: []int{kind},
				Tags:  nostr.TagMap{tagKey: []string{groupID}},
			}

			eventsCh, err := m.eventStore.QueryEvents(ctx, filter)
			if err != nil {
				return fmt.Errorf("failed to query kind %d with %s tag: %w", kind, tagKey, err)
			}

			for event := range eventsCh {
				log.Printf("  deleting old event %s (kind %d, %s tag, author %s)", event.ID[:8], kind, tagKey, event.PubKey[:8])
				if err := m.eventStore.DeleteEvent(ctx, event); err != nil {
					log.Printf("  warning: failed to delete event %s: %v", event.ID[:8], err)
				}
			}
		}
	}

	return nil
}

// generateMetadataEvents creates and saves new 39xxx events
func (m *Migrator) generateMetadataEvents(ctx context.Context, state *GroupState) error {
	// Generate 39000 (group metadata)
	if err := m.generateGroupMetadata(ctx, state); err != nil {
		return err
	}

	// Generate 39001 (admins list)
	if err := m.generateAdminsList(ctx, state); err != nil {
		return err
	}

	// Generate 39002 (members list)
	if err := m.generateMembersList(ctx, state); err != nil {
		return err
	}

	return nil
}

// generateGroupMetadata creates kind 39000 event
func (m *Migrator) generateGroupMetadata(ctx context.Context, state *GroupState) error {
	tags := nostr.Tags{
		{"d", state.ID},
	}

	// Personal groups get a "p" tag with the owner's pubkey
	// and are always private and closed
	// Also keep the "personal" tag for legacy support
	if state.PersonalOwner != "" {
		tags = append(tags, nostr.Tag{"p", state.PersonalOwner})
		tags = append(tags, nostr.Tag{"personal", state.PersonalOwner})
		tags = append(tags, nostr.Tag{"closed"})
		tags = append(tags, nostr.Tag{"private"})
		log.Printf("  personal group detected, adding p tag for owner %s (private, closed)", state.PersonalOwner[:8])
	} else {
		// Non-personal groups: add all flag tags from source events
		for flag := range state.Flags {
			tags = append(tags, nostr.Tag{flag})
		}
	}

	// Add all value tags (name, about, picture, etc.)
	for key, value := range state.Tags {
		tags = append(tags, nostr.Tag{key, value})
	}

	event := &nostr.Event{
		Kind:      groups.KindGroupMetadata,
		PubKey:    m.relayPubkey,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags:      tags,
		Content:   "",
	}

	if m.dryRun {
		isPersonal := ""
		if state.PersonalOwner != "" {
			isPersonal = ", personal group"
		}
		log.Printf("  [DRY RUN] would generate 39000 (metadata: %d tags, %d flags%s)", len(state.Tags), len(state.Flags), isPersonal)
		return nil
	}

	if err := event.Sign(m.relaySecretKey); err != nil {
		return fmt.Errorf("failed to sign metadata event: %w", err)
	}

	if err := m.eventStore.SaveEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to save metadata event: %w", err)
	}

	return nil
}

// generateAdminsList creates kind 39001 event
func (m *Migrator) generateAdminsList(ctx context.Context, state *GroupState) error {
	tags := nostr.Tags{
		{"d", state.ID},
	}

	for pubkey := range state.Admins {
		tags = append(tags, nostr.Tag{"p", pubkey, groups.RoleAdmin})
	}

	event := &nostr.Event{
		Kind:      groups.KindGroupAdmins,
		PubKey:    m.relayPubkey,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags:      tags,
		Content:   "",
	}

	if m.dryRun {
		log.Printf("  [DRY RUN] would generate 39001 (admins: %d)", len(state.Admins))
		return nil
	}

	if err := event.Sign(m.relaySecretKey); err != nil {
		return fmt.Errorf("failed to sign admins event: %w", err)
	}

	if err := m.eventStore.SaveEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to save admins event: %w", err)
	}

	return nil
}

// generateMembersList creates kind 39002 event
func (m *Migrator) generateMembersList(ctx context.Context, state *GroupState) error {
	tags := nostr.Tags{
		{"d", state.ID},
	}

	for pubkey := range state.Members {
		tags = append(tags, nostr.Tag{"p", pubkey, groups.RoleMember})
	}

	event := &nostr.Event{
		Kind:      groups.KindGroupMembers,
		PubKey:    m.relayPubkey,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags:      tags,
		Content:   "",
	}

	if m.dryRun {
		log.Printf("  [DRY RUN] would generate 39002 (members: %d)", len(state.Members))
		return nil
	}

	if err := event.Sign(m.relaySecretKey); err != nil {
		return fmt.Errorf("failed to sign members event: %w", err)
	}

	if err := m.eventStore.SaveEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to save members event: %w", err)
	}

	return nil
}

// getHTag extracts the h tag value from an event
func getHTag(event *nostr.Event) string {
	tag := event.Tags.GetFirst([]string{"h", ""})
	if tag != nil && len(*tag) >= 2 {
		return (*tag)[1]
	}
	return ""
}
