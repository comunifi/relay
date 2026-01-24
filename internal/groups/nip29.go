// Package groups implements NIP-29 relay-based group enforcement
// https://github.com/nostr-protocol/nips/blob/master/29.md
//
// This implementation enforces CLOSED groups where:
// - Creator is automatically admin
// - Users who are added become members
// - Only admins can invite/add members
// - Admins can promote members to admin
package groups

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/khatru"
	"github.com/nbd-wtf/go-nostr"
)

// Event Kinds
const (
	// Standard Nostr events
	KindProfile = 0 // User profile metadata (NIP-01)

	// NIP-28 Public Chat channel events
	KindChannelCreate   = 40 // Create channel
	KindChannelMetadata = 41 // Set channel metadata
	KindChannelMessage  = 42 // Channel message
	KindHideMessage     = 43 // Hide message
	KindMuteUser        = 44 // Mute user

	// NIP-29 Moderation events (user-generated, relay-validated)
	KindPutUser      = 9000 // Add user to group / assign role
	KindRemoveUser   = 9001 // Remove user from group
	KindEditMetadata = 9002 // Edit group metadata
	KindDeleteEvent  = 9005 // Delete event from group
	KindCreateGroup  = 9007 // Create a new group
	KindDeleteGroup  = 9008 // Delete a group
	KindCreateInvite = 9009 // Create an invite code

	// User request events
	KindJoinRequest  = 9021 // Request to join a group
	KindLeaveRequest = 9022 // Request to leave a group

	// Group content events (require h tag)
	KindGroupChat      = 9  // Short text note in group
	KindGroupReply     = 10 // Reply in group
	KindGroupThreaded  = 11 // Threaded discussion
	KindGroupChatReply = 12 // Reply to chat

	// Relay-generated metadata events
	KindGroupMetadata = 39000 // Group metadata
	KindGroupAdmins   = 39001 // Group admins list
	KindGroupMembers  = 39002 // Group members list
	KindGroupRoles    = 39003 // Group roles definition

	// Relay-generated metadata for group channels (per-channel)
	KindGroupChannels = 39004 // Group channel metadata (one event per channel)
)

// Role constants
const (
	RoleAdmin  = "admin"
	RoleMember = "member"
)

// GroupsService handles NIP-29 group enforcement
type GroupsService struct {
	eventStore     eventstore.Store
	relayPubkey    string
	relaySecretKey string
}

// NewGroupsService creates a new groups service
func NewGroupsService(eventStore eventstore.Store, relayPubkey, relaySecretKey string) *GroupsService {
	return &GroupsService{
		eventStore:     eventStore,
		relayPubkey:    relayPubkey,
		relaySecretKey: relaySecretKey,
	}
}

// AddHooks registers NIP-29 enforcement hooks on the relay
func (g *GroupsService) AddHooks(relay *khatru.Relay) {
	// Validate events before storing
	relay.RejectEvent = append(relay.RejectEvent, g.ValidateEvent)

	// After storing, generate relay metadata events for group changes
	relay.OnEventSaved = append(relay.OnEventSaved, g.OnEventSaved)

	log.Println("NIP-29 groups enforcement hooks registered")
}

// ValidateEvent validates incoming events according to NIP-29 rules for closed groups
func (g *GroupsService) ValidateEvent(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	switch event.Kind {
	case KindProfile:
		return g.validateProfile(ctx, event)
	case KindChannelCreate:
		return g.validateChannelCreate(ctx, event)
	case KindChannelMetadata:
		return g.validateChannelMetadata(ctx, event)
	case KindCreateGroup:
		return g.validateCreateGroup(ctx, event)
	case KindPutUser:
		return g.validatePutUser(ctx, event)
	case KindRemoveUser:
		return g.validateRemoveUser(ctx, event)
	case KindEditMetadata:
		return g.validateEditMetadata(ctx, event)
	case KindDeleteEvent:
		return g.validateDeleteEvent(ctx, event)
	case KindDeleteGroup:
		return g.validateDeleteGroup(ctx, event)
	case KindJoinRequest:
		return g.validateJoinRequest(ctx, event)
	case KindLeaveRequest:
		return g.validateLeaveRequest(ctx, event)
	case KindGroupChat, KindGroupReply, KindGroupThreaded, KindGroupChatReply:
		return g.validateGroupContent(ctx, event)
	case KindGroupMetadata:
		return g.validateGroupMetadataEvent(ctx, event)
	default:
		// Check if event has an h tag (group-targeted event)
		if hasHTag(event) {
			return g.validateGroupContent(ctx, event)
		}
		// Non-group events pass through
		return false, ""
	}
}

// validateCreateGroup validates group creation
// Anyone can create a group - creator becomes admin
func (g *GroupsService) validateCreateGroup(ctx context.Context, event *nostr.Event) (bool, string) {
	// Extract group ID from h tag
	groupID := getHTag(event)
	if groupID == "" {
		return true, "missing h tag (group ID) for group creation"
	}

	// Check if group already exists
	exists, err := g.groupExists(ctx, groupID)
	if err != nil {
		log.Printf("Error checking group existence: %v", err)
		return true, "internal error checking group"
	}
	if exists {
		return true, "group already exists"
	}

	return false, ""
}

// validatePutUser validates adding a user to a group or changing their role
// Only admins can add users or change roles
func (g *GroupsService) validatePutUser(ctx context.Context, event *nostr.Event) (bool, string) {
	groupID := getHTag(event)
	if groupID == "" {
		return true, "missing h tag (group ID)"
	}

	// Check if the event author is an admin
	isAdmin, err := g.IsAdmin(ctx, event.PubKey, groupID)
	if err != nil {
		log.Printf("Error checking admin status: %v", err)
		return true, "internal error checking permissions"
	}
	if !isAdmin {
		return true, "only admins can add users or change roles"
	}

	// Validate that there's at least one p tag (target user)
	pTags := getPTags(event)
	if len(pTags) == 0 {
		return true, "missing p tag (target user pubkey)"
	}

	// If promoting to admin, ensure the target is already a member
	for _, pTag := range pTags {
		targetPubkey := pTag[0]
		role := RoleMember // default role
		if len(pTag) > 1 {
			role = pTag[1]
		}

		if role == RoleAdmin {
			// To promote someone to admin, they must be a member first
			isMember, err := g.IsMember(ctx, targetPubkey, groupID)
			if err != nil {
				log.Printf("Error checking member status: %v", err)
				return true, "internal error checking membership"
			}
			if !isMember {
				return true, fmt.Sprintf("user %s must be a member before being promoted to admin", targetPubkey[:8])
			}
		}
	}

	return false, ""
}

// validateRemoveUser validates removing a user from a group
// Only admins can remove users
func (g *GroupsService) validateRemoveUser(ctx context.Context, event *nostr.Event) (bool, string) {
	groupID := getHTag(event)
	if groupID == "" {
		return true, "missing h tag (group ID)"
	}

	// Check if the event author is an admin
	isAdmin, err := g.IsAdmin(ctx, event.PubKey, groupID)
	if err != nil {
		log.Printf("Error checking admin status: %v", err)
		return true, "internal error checking permissions"
	}
	if !isAdmin {
		return true, "only admins can remove users"
	}

	// Validate that there's at least one p tag (target user)
	pTags := getPTags(event)
	if len(pTags) == 0 {
		return true, "missing p tag (target user pubkey)"
	}

	return false, ""
}

// validateEditMetadata validates editing group metadata
// Only admins can edit metadata
func (g *GroupsService) validateEditMetadata(ctx context.Context, event *nostr.Event) (bool, string) {
	groupID := getHTag(event)
	if groupID == "" {
		return true, "missing h tag (group ID)"
	}

	// Check if the event author is an admin
	isAdmin, err := g.IsAdmin(ctx, event.PubKey, groupID)
	if err != nil {
		log.Printf("Error checking admin status: %v", err)
		return true, "internal error checking permissions"
	}
	if !isAdmin {
		return true, "only admins can edit group metadata"
	}

	return false, ""
}

// validateDeleteEvent validates deleting an event from a group
// Only admins can delete events
func (g *GroupsService) validateDeleteEvent(ctx context.Context, event *nostr.Event) (bool, string) {
	groupID := getHTag(event)
	if groupID == "" {
		return true, "missing h tag (group ID)"
	}

	// Check if the event author is an admin
	isAdmin, err := g.IsAdmin(ctx, event.PubKey, groupID)
	if err != nil {
		log.Printf("Error checking admin status: %v", err)
		return true, "internal error checking permissions"
	}
	if !isAdmin {
		return true, "only admins can delete events"
	}

	return false, ""
}

// validateDeleteGroup validates deleting a group
// Only admins can delete the group
func (g *GroupsService) validateDeleteGroup(ctx context.Context, event *nostr.Event) (bool, string) {
	groupID := getHTag(event)
	if groupID == "" {
		return true, "missing h tag (group ID)"
	}

	// Check if the event author is an admin
	isAdmin, err := g.IsAdmin(ctx, event.PubKey, groupID)
	if err != nil {
		log.Printf("Error checking admin status: %v", err)
		return true, "internal error checking permissions"
	}
	if !isAdmin {
		return true, "only admins can delete the group"
	}

	return false, ""
}

// validateJoinRequest validates a request to join a group
// For closed groups, join requests are stored but don't grant access
// Admins must explicitly add users via put-user (kind 9000)
func (g *GroupsService) validateJoinRequest(ctx context.Context, event *nostr.Event) (bool, string) {
	groupID := getHTag(event)
	if groupID == "" {
		return true, "missing h tag (group ID)"
	}

	// Check if group exists
	exists, err := g.groupExists(ctx, groupID)
	if err != nil {
		log.Printf("Error checking group existence: %v", err)
		return true, "internal error checking group"
	}
	if !exists {
		return true, "group does not exist"
	}

	// Check if user is already a member (NIP-29: reject with "duplicate:" prefix)
	isMember, err := g.IsMember(ctx, event.PubKey, groupID)
	if err != nil {
		log.Printf("Error checking member status: %v", err)
		return true, "internal error checking membership"
	}
	if isMember {
		return true, "duplicate: already a member of this group"
	}

	// Allow the join request to be stored (admins can see it and act on it)
	return false, ""
}

// validateLeaveRequest validates a request to leave a group
// Members can leave voluntarily
func (g *GroupsService) validateLeaveRequest(ctx context.Context, event *nostr.Event) (bool, string) {
	groupID := getHTag(event)
	if groupID == "" {
		return true, "missing h tag (group ID)"
	}

	// Check if the user is a member
	isMember, err := g.IsMember(ctx, event.PubKey, groupID)
	if err != nil {
		log.Printf("Error checking member status: %v", err)
		return true, "internal error checking membership"
	}
	if !isMember {
		return true, "you are not a member of this group"
	}

	return false, ""
}

// validateGroupContent validates content posted to a group
// Only members can post content
func (g *GroupsService) validateGroupContent(ctx context.Context, event *nostr.Event) (bool, string) {
	groupID := getHTag(event)
	if groupID == "" {
		// Content without h tag is not group content, let it pass
		return false, ""
	}

	// Check if the user is a member (admin is also a member)
	isMember, err := g.IsMember(ctx, event.PubKey, groupID)
	if err != nil {
		log.Printf("Error checking member status: %v", err)
		return true, "internal error checking membership"
	}
	if !isMember {
		return true, "only group members can post content"
	}

	return false, ""
}

// validateChannelCreate validates creating a channel (NIP-28 kind 40) scoped to a group.
// Only group members can create channels, and an h tag (group ID) is required.
func (g *GroupsService) validateChannelCreate(ctx context.Context, event *nostr.Event) (bool, string) {
	groupID := getHTag(event)
	if groupID == "" {
		return true, "missing h tag (group ID) for channel creation"
	}

	// Ensure the group exists
	exists, err := g.groupExists(ctx, groupID)
	if err != nil {
		log.Printf("Error checking group existence for channel creation: %v", err)
		return true, "internal error checking group"
	}
	if !exists {
		return true, "group does not exist"
	}

	// Only members can create channels
	isMember, err := g.IsMember(ctx, event.PubKey, groupID)
	if err != nil {
		log.Printf("Error checking member status for channel creation: %v", err)
		return true, "internal error checking membership"
	}
	if !isMember {
		return true, "only group members can create channels"
	}

	// Validate that content, if present, is valid JSON per NIP-28
	if event.Content != "" {
		var meta struct {
			Name    string   `json:"name,omitempty"`
			About   string   `json:"about,omitempty"`
			Picture string   `json:"picture,omitempty"`
			Relays  []string `json:"relays,omitempty"`
		}
		if err := json.Unmarshal([]byte(event.Content), &meta); err != nil {
			return true, "invalid channel metadata content (must be JSON)"
		}
	}

	return false, ""
}

// validateChannelMetadata validates updating channel metadata (NIP-28 kind 41) scoped to a group.
// Only group members can modify channel metadata. Requires h tag (group ID) and e tag (channel ID).
func (g *GroupsService) validateChannelMetadata(ctx context.Context, event *nostr.Event) (bool, string) {
	groupID := getHTag(event)
	if groupID == "" {
		return true, "missing h tag (group ID) for channel metadata"
	}

	// Ensure the group exists
	exists, err := g.groupExists(ctx, groupID)
	if err != nil {
		log.Printf("Error checking group existence for channel metadata: %v", err)
		return true, "internal error checking group"
	}
	if !exists {
		return true, "group does not exist"
	}

	// Only members can modify channel metadata
	isMember, err := g.IsMember(ctx, event.PubKey, groupID)
	if err != nil {
		log.Printf("Error checking member status for channel metadata: %v", err)
		return true, "internal error checking membership"
	}
	if !isMember {
		return true, "only group members can edit channel metadata"
	}

	// Require an e tag that points to the channel's create event (kind 40)
	channelID := getFirstETag(event)
	if channelID == "" {
		return true, "missing e tag (channel event id)"
	}

	// Optionally verify the channel event exists and belongs to the same group
	if ok, err := g.channelBelongsToGroup(ctx, channelID, groupID); err != nil {
		log.Printf("Error verifying channel %s belongs to group %s: %v", channelID, groupID, err)
		return true, "internal error verifying channel"
	} else if !ok {
		return true, "channel does not belong to this group"
	}

	// Validate that content, if present, is valid JSON per NIP-28
	if event.Content != "" {
		var meta struct {
			Name    string   `json:"name,omitempty"`
			About   string   `json:"about,omitempty"`
			Picture string   `json:"picture,omitempty"`
			Relays  []string `json:"relays,omitempty"`
		}
		if err := json.Unmarshal([]byte(event.Content), &meta); err != nil {
			return true, "invalid channel metadata content (must be JSON)"
		}
	}

	return false, ""
}

// validateProfile validates kind 0 profile events for username uniqueness
// The username is taken from the "u" tag and must be unique across all users
func (g *GroupsService) validateProfile(ctx context.Context, event *nostr.Event) (bool, string) {
	username := getUTag(event)
	if username == "" {
		// No username tag, allow the profile update
		return false, ""
	}

	// Query for existing kind 0 events with this u tag
	profileFilter := nostr.Filter{
		Kinds: []int{KindProfile},
		Tags:  nostr.TagMap{"u": []string{username}},
		Limit: 10,
	}

	events, err := g.eventStore.QueryEvents(ctx, profileFilter)
	if err != nil {
		log.Printf("Error checking username uniqueness: %v", err)
		return true, "internal error checking username"
	}

	for evt := range events {
		// If we find a profile with this username from a different pubkey, reject
		if evt.PubKey != event.PubKey {
			return true, fmt.Sprintf("username '%s' is already taken", username)
		}
	}

	// Username is available or belongs to the same user (updating their profile)
	return false, ""
}

// validateGroupMetadataEvent validates kind 39000 group metadata events
// For personal groups, the p tag must match the author's pubkey
func (g *GroupsService) validateGroupMetadataEvent(ctx context.Context, event *nostr.Event) (bool, string) {
	// Check if this is a personal group (has p tag)
	ownerPubkey := getFirstPTag(event)
	if ownerPubkey != "" {
		// Personal group: p tag must match the author
		if ownerPubkey != event.PubKey {
			return true, "cannot create personal group for another user"
		}
	}

	// Allow the event
	return false, ""
}

// OnEventSaved is called after an event is successfully stored
// It generates relay metadata events for group changes
func (g *GroupsService) OnEventSaved(ctx context.Context, event *nostr.Event) {
	switch event.Kind {
	case KindCreateGroup:
		g.handleGroupCreated(ctx, event)
	case KindPutUser:
		g.handleUserAdded(ctx, event)
	case KindRemoveUser:
		g.handleUserRemoved(ctx, event)
	case KindEditMetadata:
		g.handleMetadataEdited(ctx, event)
	case KindLeaveRequest:
		g.handleUserLeft(ctx, event)
	case KindChannelCreate:
		g.handleChannelCreated(ctx, event)
	case KindChannelMetadata:
		g.handleChannelMetadataUpdated(ctx, event)
	}
}

// handleGroupCreated processes a new group creation
// Creates initial metadata and makes creator an admin
func (g *GroupsService) handleGroupCreated(ctx context.Context, event *nostr.Event) {
	groupID := getHTag(event)
	if groupID == "" {
		return
	}

	log.Printf("Group %s created by %s", groupID, event.PubKey[:8])

	// Generate group metadata event (kind 39000)
	g.generateGroupMetadata(ctx, groupID, event)

	// Generate admins list with creator as admin (kind 39001)
	g.generateAdminsList(ctx, groupID, []string{event.PubKey})

	// Generate initial empty members list (kind 39002)
	// Note: the creator is admin, not just a member
	g.generateMembersList(ctx, groupID, []string{})
}

// handleUserAdded processes when a user is added to a group
func (g *GroupsService) handleUserAdded(ctx context.Context, event *nostr.Event) {
	groupID := getHTag(event)
	if groupID == "" {
		return
	}

	pTags := getPTags(event)
	for _, pTag := range pTags {
		targetPubkey := pTag[0]
		role := RoleMember
		if len(pTag) > 1 {
			role = pTag[1]
		}

		log.Printf("User %s added to group %s with role %s by %s",
			targetPubkey[:8], groupID, role, event.PubKey[:8])

		if role == RoleAdmin {
			// Update admins list
			admins, _ := g.getAdmins(ctx, groupID)
			admins = appendUnique(admins, targetPubkey)
			g.generateAdminsList(ctx, groupID, admins)
		}

		// Always update members list (admins are also members)
		members, _ := g.getMembers(ctx, groupID)
		members = appendUnique(members, targetPubkey)
		g.generateMembersList(ctx, groupID, members)
	}
}

// handleUserRemoved processes when a user is removed from a group
func (g *GroupsService) handleUserRemoved(ctx context.Context, event *nostr.Event) {
	groupID := getHTag(event)
	if groupID == "" {
		return
	}

	pTags := getPTags(event)
	for _, pTag := range pTags {
		targetPubkey := pTag[0]

		log.Printf("User %s removed from group %s by %s",
			targetPubkey[:8], groupID, event.PubKey[:8])

		// Remove from admins list if present
		admins, _ := g.getAdmins(ctx, groupID)
		admins = removeFromSlice(admins, targetPubkey)
		g.generateAdminsList(ctx, groupID, admins)

		// Remove from members list
		members, _ := g.getMembers(ctx, groupID)
		members = removeFromSlice(members, targetPubkey)
		g.generateMembersList(ctx, groupID, members)
	}
}

// handleUserLeft processes when a user voluntarily leaves a group
// Per NIP-29: "The relay will automatically issue a kind:9001 in response removing this user"
func (g *GroupsService) handleUserLeft(ctx context.Context, event *nostr.Event) {
	groupID := getHTag(event)
	if groupID == "" {
		return
	}

	log.Printf("User %s left group %s", event.PubKey[:8], groupID)

	// Generate kind 9001 (remove-user) event signed by relay
	// This follows NIP-29 which requires the relay to issue a 9001 in response
	removeEvent := &nostr.Event{
		Kind:      KindRemoveUser,
		PubKey:    g.relayPubkey,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"h", groupID},
			{"p", event.PubKey},
		},
		Content: "user left the group",
	}

	if err := removeEvent.Sign(g.relaySecretKey); err != nil {
		log.Printf("Error signing remove-user event for leave request: %v", err)
		return
	}

	if err := g.eventStore.SaveEvent(ctx, removeEvent); err != nil {
		log.Printf("Error saving remove-user event for leave request: %v", err)
		return
	}

	// handleUserRemoved will be triggered by OnEventSaved for the 9001 event
	// which will update the 39001 (admins) and 39002 (members) lists
}

// handleMetadataEdited processes when group metadata is edited
func (g *GroupsService) handleMetadataEdited(ctx context.Context, event *nostr.Event) {
	groupID := getHTag(event)
	if groupID == "" {
		return
	}

	log.Printf("Group %s metadata edited by %s", groupID, event.PubKey[:8])

	// Regenerate group metadata from the edit event
	g.generateGroupMetadata(ctx, groupID, event)
}

// handleChannelCreated processes a new NIP-28 channel creation event (kind 40) scoped to a group.
// It creates or updates a per-channel relay metadata event (kind 39004) for this (group, channel).
func (g *GroupsService) handleChannelCreated(ctx context.Context, event *nostr.Event) {
	groupID := getHTag(event)
	if groupID == "" {
		return
	}

	// Log with safe truncation in case IDs are shorter than 8 characters (e.g. in tests).
	idLog := event.ID
	if len(idLog) > 8 {
		idLog = idLog[:8]
	}
	pubLog := event.PubKey
	if len(pubLog) > 8 {
		pubLog = pubLog[:8]
	}
	log.Printf("Channel %s created in group %s by %s", idLog, groupID, pubLog)

	// Parse basic channel metadata from event content (NIP-28)
	var content struct {
		Name    string   `json:"name,omitempty"`
		About   string   `json:"about,omitempty"`
		Picture string   `json:"picture,omitempty"`
		Relays  []string `json:"relays,omitempty"`
	}
	if event.Content != "" {
		if err := json.Unmarshal([]byte(event.Content), &content); err != nil {
			log.Printf("Error parsing channel metadata content for channel %s: %v", event.ID[:8], err)
		}
	}

	channel := ChannelMetadata{
		ID:      event.ID,
		GroupID: groupID,
		Name:    content.Name,
		About:   content.About,
		Picture: content.Picture,
		Relays:  content.Relays,
		Creator: event.PubKey,
	}

	g.generateChannelMetadataEvent(ctx, groupID, &channel)
}

// handleChannelMetadataUpdated processes a NIP-28 channel metadata event (kind 41) scoped to a group.
// It updates the per-channel relay metadata event (kind 39004) for this (group, channel).
func (g *GroupsService) handleChannelMetadataUpdated(ctx context.Context, event *nostr.Event) {
	groupID := getHTag(event)
	if groupID == "" {
		return
	}

	channelID := getFirstETag(event)
	if channelID == "" {
		return
	}

	// Log with safe truncation in case IDs are shorter than 8 characters (e.g. in tests).
	chIDLog := channelID
	if len(chIDLog) > 8 {
		chIDLog = chIDLog[:8]
	}
	pubLog := event.PubKey
	if len(pubLog) > 8 {
		pubLog = pubLog[:8]
	}
	log.Printf("Channel %s metadata updated in group %s by %s", chIDLog, groupID, pubLog)

	// Parse updated channel metadata from event content (NIP-28)
	var content struct {
		Name    string   `json:"name,omitempty"`
		About   string   `json:"about,omitempty"`
		Picture string   `json:"picture,omitempty"`
		Relays  []string `json:"relays,omitempty"`
	}
	if event.Content != "" {
		if err := json.Unmarshal([]byte(event.Content), &content); err != nil {
			log.Printf("Error parsing channel metadata update content for channel %s: %v", chIDLog, err)
		}
	}

	// Load existing metadata for this channel so we can preserve creator, etc.
	existingMeta, err := g.getGroupChannelsMetadata(ctx, groupID)
	if err != nil {
		log.Printf("Error loading group channels metadata for group %s: %v", groupID, err)
		return
	}

	var existing *ChannelMetadata
	for i := range existingMeta.Channels {
		ch := &existingMeta.Channels[i]
		if ch.ID == channelID {
			existing = ch
			break
		}
	}

	channel := ChannelMetadata{
		ID:      channelID,
		GroupID: groupID,
		Name:    content.Name,
		About:   content.About,
		Picture: content.Picture,
		Relays:  content.Relays,
	}

	// Preserve creator from existing metadata if available.
	if existing != nil {
		channel.Creator = existing.Creator
	}

	g.generateChannelMetadataEvent(ctx, groupID, &channel)
}

// deleteOldMetadataEvent removes existing events of the given kind for a group, excluding a specific event ID
func (g *GroupsService) deleteOldMetadataEvent(ctx context.Context, kind int, groupID string, excludeEventID string) {
	// Check both "d" and "h" tags to catch any legacy events
	for _, tagKey := range []string{"d", "h"} {
		filter := nostr.Filter{
			Kinds: []int{kind},
			Tags:  nostr.TagMap{tagKey: []string{groupID}},
		}

		eventsCh, err := g.eventStore.QueryEvents(ctx, filter)
		if err != nil {
			log.Printf("Error querying old %d events: %v", kind, err)
			continue
		}

		for event := range eventsCh {
			// Skip the newly created event
			if event.ID == excludeEventID {
				continue
			}
			if err := g.eventStore.DeleteEvent(ctx, event); err != nil {
				log.Printf("Error deleting old event %s: %v", event.ID[:8], err)
			}
		}
	}
}

// channelBelongsToGroup verifies that a channel (kind 40) event exists and is associated with the given group ID.
func (g *GroupsService) channelBelongsToGroup(ctx context.Context, channelID, groupID string) (bool, error) {
	filter := nostr.Filter{
		IDs:   []string{channelID},
		Limit: 1,
	}

	eventsCh, err := g.eventStore.QueryEvents(ctx, filter)
	if err != nil {
		return false, err
	}

	for evt := range eventsCh {
		if evt.Kind != KindChannelCreate {
			continue
		}
		// Check the h tag on the channel create event
		if getHTag(evt) == groupID {
			return true, nil
		}
	}

	return false, nil
}

// generateGroupMetadata creates/updates a kind 39000 group metadata event
func (g *GroupsService) generateGroupMetadata(ctx context.Context, groupID string, sourceEvent *nostr.Event) {
	// Extract metadata from source event tags
	// We preserve all tags except structural ones (h, p, e, client, client_sig)
	skipTags := map[string]bool{
		"h":          true,
		"p":          true,
		"e":          true,
		"client":     true,
		"client_sig": true,
	}

	tags := nostr.Tags{
		{"d", groupID},
	}

	// Check for personal group tag and add p tag if present
	personalOwner := ""
	hasOpenFlag := false
	hasPublicFlag := false

	for _, tag := range sourceEvent.Tags {
		if len(tag) >= 2 && tag[0] == "personal" {
			personalOwner = tag[1]
		}
		if len(tag) >= 1 && tag[0] == "open" {
			hasOpenFlag = true
		}
		if len(tag) >= 1 && tag[0] == "public" {
			hasPublicFlag = true
		}
	}

	// Personal groups get a "p" tag with the owner's pubkey
	// and are always private and closed
	// Also keep the "personal" tag for legacy support
	if personalOwner != "" {
		tags = append(tags, nostr.Tag{"p", personalOwner})
		tags = append(tags, nostr.Tag{"personal", personalOwner})
		tags = append(tags, nostr.Tag{"closed"})
		tags = append(tags, nostr.Tag{"private"})
	} else {
		// Non-personal groups: use flags from source event with defaults
		if hasOpenFlag {
			tags = append(tags, nostr.Tag{"open"})
		} else {
			tags = append(tags, nostr.Tag{"closed"})
		}

		if hasPublicFlag {
			tags = append(tags, nostr.Tag{"public"})
		} else {
			tags = append(tags, nostr.Tag{"private"})
		}
	}

	// Add all other metadata tags from source event
	for _, tag := range sourceEvent.Tags {
		if len(tag) == 0 {
			continue
		}
		tagName := tag[0]

		// Skip structural tags and flags we already handled
		if skipTags[tagName] || tagName == "personal" || tagName == "open" || tagName == "public" || tagName == "closed" || tagName == "private" {
			continue
		}

		// Add the tag as-is
		if len(tag) >= 2 && tag[1] != "" {
			tags = append(tags, nostr.Tag{tagName, tag[1]})
		}
	}

	metadata := &nostr.Event{
		Kind:      KindGroupMetadata,
		PubKey:    g.relayPubkey,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags:      tags,
		Content:   "",
	}

	if err := metadata.Sign(g.relaySecretKey); err != nil {
		log.Printf("Error signing group metadata event: %v", err)
		return
	}

	if err := g.eventStore.SaveEvent(ctx, metadata); err != nil {
		log.Printf("Error saving group metadata event: %v", err)
		return
	}

	// Delete old metadata events after successful save
	g.deleteOldMetadataEvent(ctx, KindGroupMetadata, groupID, metadata.ID)
}

// generateAdminsList creates/updates a kind 39001 admins list event
func (g *GroupsService) generateAdminsList(ctx context.Context, groupID string, admins []string) {
	tags := nostr.Tags{
		{"d", groupID},
	}

	for _, admin := range admins {
		tags = append(tags, nostr.Tag{"p", admin, RoleAdmin})
	}

	event := &nostr.Event{
		Kind:      KindGroupAdmins,
		PubKey:    g.relayPubkey,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags:      tags,
		Content:   "",
	}

	if err := event.Sign(g.relaySecretKey); err != nil {
		log.Printf("Error signing admins list event: %v", err)
		return
	}

	if err := g.eventStore.SaveEvent(ctx, event); err != nil {
		log.Printf("Error saving admins list event: %v", err)
		return
	}

	// Delete old admins list events after successful save
	g.deleteOldMetadataEvent(ctx, KindGroupAdmins, groupID, event.ID)
}

// generateMembersList creates/updates a kind 39002 members list event
func (g *GroupsService) generateMembersList(ctx context.Context, groupID string, members []string) {
	tags := nostr.Tags{
		{"d", groupID},
	}

	for _, member := range members {
		tags = append(tags, nostr.Tag{"p", member, RoleMember})
	}

	event := &nostr.Event{
		Kind:      KindGroupMembers,
		PubKey:    g.relayPubkey,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags:      tags,
		Content:   "",
	}

	if err := event.Sign(g.relaySecretKey); err != nil {
		log.Printf("Error signing members list event: %v", err)
		return
	}

	if err := g.eventStore.SaveEvent(ctx, event); err != nil {
		log.Printf("Error saving members list event: %v", err)
		return
	}

	// Delete old members list events after successful save
	g.deleteOldMetadataEvent(ctx, KindGroupMembers, groupID, event.ID)
}

// IsAdmin checks if a pubkey is an admin of a group
func (g *GroupsService) IsAdmin(ctx context.Context, pubkey, groupID string) (bool, error) {
	// First check relay-generated admins list (kind 39001)
	adminsFilter := nostr.Filter{
		Kinds:   []int{KindGroupAdmins},
		Authors: []string{g.relayPubkey},
		Tags:    nostr.TagMap{"d": []string{groupID}},
		Limit:   1,
	}

	events, err := g.eventStore.QueryEvents(ctx, adminsFilter)
	if err != nil {
		return false, fmt.Errorf("failed to query admins list: %w", err)
	}

	for evt := range events {
		for _, tag := range evt.Tags {
			if len(tag) >= 2 && tag[0] == "p" && tag[1] == pubkey {
				return true, nil
			}
		}
	}

	// Fallback: Check moderation events (put-user/remove-user)
	// This handles the case where the relay metadata hasn't been generated yet
	return g.isAdminFromModEvents(ctx, pubkey, groupID)
}

// isAdminFromModEvents checks admin status from moderation events
func (g *GroupsService) isAdminFromModEvents(ctx context.Context, pubkey, groupID string) (bool, error) {
	// Check if this user created the group (kind 9007)
	createFilter := nostr.Filter{
		Kinds:   []int{KindCreateGroup},
		Authors: []string{pubkey},
		Tags:    nostr.TagMap{"h": []string{groupID}},
		Limit:   1,
	}

	createEvents, err := g.eventStore.QueryEvents(ctx, createFilter)
	if err != nil {
		return false, err
	}

	for range createEvents {
		// User created the group, they're admin
		return true, nil
	}

	// Check put-user events that assigned admin role to this user
	modFilter := nostr.Filter{
		Kinds: []int{KindPutUser},
		Tags: nostr.TagMap{
			"h": []string{groupID},
			"p": []string{pubkey},
		},
		Limit: 100,
	}

	modEvents, err := g.eventStore.QueryEvents(ctx, modFilter)
	if err != nil {
		return false, err
	}

	var latestPut *nostr.Event
	for evt := range modEvents {
		if latestPut == nil || evt.CreatedAt > latestPut.CreatedAt {
			latestPut = evt
		}
	}

	if latestPut != nil {
		// Check if the role is admin
		for _, tag := range latestPut.Tags {
			if len(tag) >= 3 && tag[0] == "p" && tag[1] == pubkey && tag[2] == RoleAdmin {
				// Need to verify they weren't removed after
				return g.checkNotRemoved(ctx, pubkey, groupID, latestPut.CreatedAt)
			}
		}
	}

	return false, nil
}

// IsMember checks if a pubkey is a member of a group (includes admins)
func (g *GroupsService) IsMember(ctx context.Context, pubkey, groupID string) (bool, error) {
	// Admins are also members
	isAdmin, err := g.IsAdmin(ctx, pubkey, groupID)
	if err != nil {
		return false, err
	}
	if isAdmin {
		return true, nil
	}

	// Check relay-generated members list (kind 39002)
	membersFilter := nostr.Filter{
		Kinds:   []int{KindGroupMembers},
		Authors: []string{g.relayPubkey},
		Tags:    nostr.TagMap{"d": []string{groupID}},
		Limit:   1,
	}

	events, err := g.eventStore.QueryEvents(ctx, membersFilter)
	if err != nil {
		return false, fmt.Errorf("failed to query members list: %w", err)
	}

	for evt := range events {
		for _, tag := range evt.Tags {
			if len(tag) >= 2 && tag[0] == "p" && tag[1] == pubkey {
				return true, nil
			}
		}
	}

	// Fallback: Check moderation events
	return g.isMemberFromModEvents(ctx, pubkey, groupID)
}

// isMemberFromModEvents checks membership from moderation events
func (g *GroupsService) isMemberFromModEvents(ctx context.Context, pubkey, groupID string) (bool, error) {
	// Check the latest put-user or remove-user event for this user
	modFilter := nostr.Filter{
		Kinds: []int{KindPutUser, KindRemoveUser},
		Tags: nostr.TagMap{
			"h": []string{groupID},
			"p": []string{pubkey},
		},
		Limit: 10,
	}

	events, err := g.eventStore.QueryEvents(ctx, modFilter)
	if err != nil {
		return false, err
	}

	var latestEvent *nostr.Event
	for evt := range events {
		if latestEvent == nil || evt.CreatedAt > latestEvent.CreatedAt {
			latestEvent = evt
		}
	}

	if latestEvent == nil {
		return false, nil
	}

	// Member if latest event is put-user, not member if remove-user
	return latestEvent.Kind == KindPutUser, nil
}

// checkNotRemoved verifies the user wasn't removed after a certain time
func (g *GroupsService) checkNotRemoved(ctx context.Context, pubkey, groupID string, afterTime nostr.Timestamp) (bool, error) {
	removeFilter := nostr.Filter{
		Kinds: []int{KindRemoveUser},
		Tags: nostr.TagMap{
			"h": []string{groupID},
			"p": []string{pubkey},
		},
		Since: &afterTime,
		Limit: 1,
	}

	events, err := g.eventStore.QueryEvents(ctx, removeFilter)
	if err != nil {
		return false, err
	}

	for range events {
		// User was removed after being added
		return false, nil
	}

	return true, nil
}

// groupExists checks if a group exists
func (g *GroupsService) groupExists(ctx context.Context, groupID string) (bool, error) {
	// Check for group metadata
	metaFilter := nostr.Filter{
		Kinds: []int{KindGroupMetadata},
		Tags:  nostr.TagMap{"d": []string{groupID}},
		Limit: 1,
	}

	events, err := g.eventStore.QueryEvents(ctx, metaFilter)
	if err != nil {
		return false, err
	}

	for range events {
		return true, nil
	}

	// Also check for create-group events
	createFilter := nostr.Filter{
		Kinds: []int{KindCreateGroup},
		Tags:  nostr.TagMap{"h": []string{groupID}},
		Limit: 1,
	}

	events, err = g.eventStore.QueryEvents(ctx, createFilter)
	if err != nil {
		return false, err
	}

	for range events {
		return true, nil
	}

	return false, nil
}

// getAdmins returns the list of admin pubkeys for a group
func (g *GroupsService) getAdmins(ctx context.Context, groupID string) ([]string, error) {
	adminsFilter := nostr.Filter{
		Kinds:   []int{KindGroupAdmins},
		Authors: []string{g.relayPubkey},
		Tags:    nostr.TagMap{"d": []string{groupID}},
		Limit:   1,
	}

	events, err := g.eventStore.QueryEvents(ctx, adminsFilter)
	if err != nil {
		return nil, err
	}

	var admins []string
	for evt := range events {
		for _, tag := range evt.Tags {
			if len(tag) >= 2 && tag[0] == "p" {
				admins = append(admins, tag[1])
			}
		}
	}

	return admins, nil
}

// getMembers returns the list of member pubkeys for a group (not including admins)
func (g *GroupsService) getMembers(ctx context.Context, groupID string) ([]string, error) {
	membersFilter := nostr.Filter{
		Kinds:   []int{KindGroupMembers},
		Authors: []string{g.relayPubkey},
		Tags:    nostr.TagMap{"d": []string{groupID}},
		Limit:   1,
	}

	events, err := g.eventStore.QueryEvents(ctx, membersFilter)
	if err != nil {
		return nil, err
	}

	var members []string
	for evt := range events {
		for _, tag := range evt.Tags {
			if len(tag) >= 2 && tag[0] == "p" {
				members = append(members, tag[1])
			}
		}
	}

	return members, nil
}

// Helper functions

func hasHTag(event *nostr.Event) bool {
	return getHTag(event) != ""
}

func getHTag(event *nostr.Event) string {
	tag := event.Tags.GetFirst([]string{"h", ""})
	if tag != nil && len(*tag) >= 2 {
		return (*tag)[1]
	}
	return ""
}

// getUTag extracts the username from the "u" tag
func getUTag(event *nostr.Event) string {
	tag := event.Tags.GetFirst([]string{"u", ""})
	if tag != nil && len(*tag) >= 2 {
		return (*tag)[1]
	}
	return ""
}

// getFirstPTag extracts the first p tag value (used for personal group owner)
func getFirstPTag(event *nostr.Event) string {
	tag := event.Tags.GetFirst([]string{"p", ""})
	if tag != nil && len(*tag) >= 2 {
		return (*tag)[1]
	}
	return ""
}

// getFirstETag extracts the first e tag value (used for channel metadata referencing the channel event).
func getFirstETag(event *nostr.Event) string {
	tag := event.Tags.GetFirst([]string{"e", ""})
	if tag != nil && len(*tag) >= 2 {
		return (*tag)[1]
	}
	return ""
}

// getPTags returns all p tag values with their optional role
// Returns slice of [pubkey, role?]
func getPTags(event *nostr.Event) [][]string {
	var result [][]string
	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == "p" {
			entry := []string{tag[1]}
			if len(tag) >= 3 {
				entry = append(entry, tag[2])
			}
			result = append(result, entry)
		}
	}
	return result
}

func appendUnique(slice []string, item string) []string {
	for _, s := range slice {
		if s == item {
			return slice
		}
	}
	return append(slice, item)
}

func removeFromSlice(slice []string, item string) []string {
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if s != item {
			result = append(result, s)
		}
	}
	return result
}

// GroupMetadata represents group metadata for JSON serialization
type GroupMetadata struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	About   string `json:"about,omitempty"`
	Picture string `json:"picture,omitempty"`
	Closed  bool   `json:"closed"`
	Private bool   `json:"private"`
}

// GetGroupMetadata retrieves metadata for a group
func (g *GroupsService) GetGroupMetadata(ctx context.Context, groupID string) (*GroupMetadata, error) {
	metaFilter := nostr.Filter{
		Kinds: []int{KindGroupMetadata},
		Tags:  nostr.TagMap{"d": []string{groupID}},
		Limit: 1,
	}

	events, err := g.eventStore.QueryEvents(ctx, metaFilter)
	if err != nil {
		return nil, err
	}

	var evt *nostr.Event
	for e := range events {
		evt = e
		break
	}
	if evt == nil {
		return nil, fmt.Errorf("group not found")
	}

	meta := &GroupMetadata{
		ID:      groupID,
		Closed:  true, // All our groups are closed
		Private: true,
	}

	for _, tag := range evt.Tags {
		if len(tag) >= 2 {
			switch tag[0] {
			case "name":
				meta.Name = tag[1]
			case "about":
				meta.About = tag[1]
			case "picture":
				meta.Picture = tag[1]
			}
		}
	}

	return meta, nil
}

// SerializeMetadata serializes group metadata to JSON
func (m *GroupMetadata) SerializeMetadata() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ChannelMetadata represents metadata for a single channel within a group.
type ChannelMetadata struct {
	ID      string   `json:"id"`
	GroupID string   `json:"group_id,omitempty"`
	Name    string   `json:"name,omitempty"`
	About   string   `json:"about,omitempty"`
	Picture string   `json:"picture,omitempty"`
	Relays  []string `json:"relays,omitempty"`
	Creator string   `json:"creator,omitempty"`
}

// GroupChannelsMetadata represents all channels for a given group.
type GroupChannelsMetadata struct {
	GroupID  string            `json:"group_id"`
	Channels []ChannelMetadata `json:"channels"`
}

// Serialize serializes group channels metadata to JSON.
func (m *GroupChannelsMetadata) Serialize() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// DeserializeGroupChannelsMetadata deserializes group channels metadata from JSON.
func DeserializeGroupChannelsMetadata(content string) (*GroupChannelsMetadata, error) {
	if content == "" {
		return &GroupChannelsMetadata{}, nil
	}

	var meta GroupChannelsMetadata
	if err := json.Unmarshal([]byte(content), &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// GetGroupChannels retrieves channel metadata for a group by aggregating
// all relay-generated per-channel metadata events (kind 39004) for that group.
func (g *GroupsService) GetGroupChannels(ctx context.Context, groupID string) (*GroupChannelsMetadata, error) {
	return g.getGroupChannelsMetadata(ctx, groupID)
}

// getGroupChannelsMetadata aggregates per-channel relay metadata events (kind 39004)
// for a group into a GroupChannelsMetadata structure.
func (g *GroupsService) getGroupChannelsMetadata(ctx context.Context, groupID string) (*GroupChannelsMetadata, error) {
	filter := nostr.Filter{
		Kinds:   []int{KindGroupChannels},
		Authors: []string{g.relayPubkey},
		Tags:    nostr.TagMap{"h": []string{groupID}},
	}

	events, err := g.eventStore.QueryEvents(ctx, filter)
	if err != nil {
		return nil, err
	}

	meta := &GroupChannelsMetadata{
		GroupID:  groupID,
		Channels: []ChannelMetadata{},
	}

	for evt := range events {
		var ch ChannelMetadata
		if err := json.Unmarshal([]byte(evt.Content), &ch); err != nil {
			log.Printf("Error decoding channel metadata for group %s: %v", groupID, err)
			continue
		}
		// Ensure group ID is set on the struct
		if ch.GroupID == "" {
			ch.GroupID = groupID
		}
		meta.Channels = append(meta.Channels, ch)
	}

	return meta, nil
}

// generateChannelMetadataEvent creates/updates a per-channel KindGroupChannels event
// for a given (groupID, channel.ID) pair. It uses a \"d\" tag of the form
// \"<groupID>:<channelID>\" so the event is addressable and replaceable.
func (g *GroupsService) generateChannelMetadataEvent(ctx context.Context, groupID string, channel *ChannelMetadata) {
	if channel == nil {
		return
	}
	if channel.ID == "" {
		log.Printf("Cannot generate channel metadata event for group %s: missing channel ID", groupID)
		return
	}
	if channel.GroupID == "" {
		channel.GroupID = groupID
	}

	content, err := json.Marshal(channel)
	if err != nil {
		log.Printf("Error serializing channel metadata for group %s, channel %s: %v", groupID, channel.ID[:8], err)
		return
	}

	key := fmt.Sprintf("%s:%s", groupID, channel.ID)

	event := &nostr.Event{
		Kind:      KindGroupChannels,
		PubKey:    g.relayPubkey,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"h", groupID},
			{"d", key},
			{"e", channel.ID},
		},
		Content: string(content),
	}

	if err := event.Sign(g.relaySecretKey); err != nil {
		log.Printf("Error signing channel metadata event for group %s, channel %s: %v", groupID, channel.ID[:8], err)
		return
	}

	// Use ReplaceEvent so there is exactly one metadata event per (groupID, channelID).
	if err := g.eventStore.ReplaceEvent(ctx, event); err != nil {
		log.Printf("Error saving channel metadata event for group %s, channel %s: %v", groupID, channel.ID[:8], err)
		return
	}
}
