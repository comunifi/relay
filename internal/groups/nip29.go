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

// NIP-29 Event Kinds
const (
	// Moderation events (user-generated, relay-validated)
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
	KindGroupChat      = 9   // Short text note in group
	KindGroupReply     = 10  // Reply in group
	KindGroupThreaded  = 11  // Threaded discussion
	KindGroupChatReply = 12  // Reply to chat

	// Relay-generated metadata events
	KindGroupMetadata = 39000 // Group metadata
	KindGroupAdmins   = 39001 // Group admins list
	KindGroupMembers  = 39002 // Group members list
	KindGroupRoles    = 39003 // Group roles definition
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
func (g *GroupsService) handleUserLeft(ctx context.Context, event *nostr.Event) {
	groupID := getHTag(event)
	if groupID == "" {
		return
	}

	log.Printf("User %s left group %s", event.PubKey[:8], groupID)

	// Remove from admins list if present
	admins, _ := g.getAdmins(ctx, groupID)
	admins = removeFromSlice(admins, event.PubKey)
	g.generateAdminsList(ctx, groupID, admins)

	// Remove from members list
	members, _ := g.getMembers(ctx, groupID)
	members = removeFromSlice(members, event.PubKey)
	g.generateMembersList(ctx, groupID, members)
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

// generateGroupMetadata creates/updates a kind 39000 group metadata event
func (g *GroupsService) generateGroupMetadata(ctx context.Context, groupID string, sourceEvent *nostr.Event) {
	// Extract metadata from source event tags
	name := ""
	about := ""
	picture := ""

	for _, tag := range sourceEvent.Tags {
		if len(tag) >= 2 {
			switch tag[0] {
			case "name":
				name = tag[1]
			case "about":
				about = tag[1]
			case "picture":
				picture = tag[1]
			}
		}
	}

	tags := nostr.Tags{
		{"d", groupID},
		{"closed"},        // All groups are closed
		{"private"},       // Groups are private by default
	}
	if name != "" {
		tags = append(tags, nostr.Tag{"name", name})
	}
	if about != "" {
		tags = append(tags, nostr.Tag{"about", about})
	}
	if picture != "" {
		tags = append(tags, nostr.Tag{"picture", picture})
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
	}
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
	}
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
	}
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

	for evt := range events {
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

	return nil, fmt.Errorf("group not found")
}

// SerializeMetadata serializes group metadata to JSON
func (m *GroupMetadata) SerializeMetadata() (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

