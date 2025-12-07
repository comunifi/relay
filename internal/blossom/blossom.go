package blossom

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/khatru"
	"github.com/fiatjaf/khatru/blossom"
	"github.com/nbd-wtf/go-nostr"
)

const (
	// MaxFileSize is the maximum allowed upload size (50MB)
	MaxFileSize = 50 * 1024 * 1024

	// NIP-29 group membership event kinds
	KindGroupAddUser    = 9000
	KindGroupRemoveUser = 9001
	KindGroupMembers    = 39002 // Group members list
)

type BlossomConfig struct {
	ServiceURL      string
	AWSAccessKeyID  string
	AWSSecretKey    string
	AWSRegion       string
	AWSEndpointURL  string
	AWSS3BucketName string
}

type BlossomService struct {
	config     *BlossomConfig
	s3Client   *s3.Client
	blossom    *blossom.BlossomServer
	eventStore eventstore.Store

	// pendingUploads maps sha256 -> groupID for uploads in progress
	pendingUploads sync.Map
}

// NewBlossomService creates a new blossom service with S3 backend
func NewBlossomService(ctx context.Context, relay *khatru.Relay, blobStore eventstore.Store, cfg *BlossomConfig) (*BlossomService, error) {
	// Create S3 client
	s3Client, err := createS3Client(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}

	// Create blossom server
	bl := blossom.New(relay, cfg.ServiceURL)

	// Set up blob metadata store
	bl.Store = blossom.EventStoreBlobIndexWrapper{
		Store:      blobStore,
		ServiceURL: bl.ServiceURL,
	}

	service := &BlossomService{
		config:     cfg,
		s3Client:   s3Client,
		blossom:    bl,
		eventStore: blobStore,
	}

	// Configure storage functions
	bl.StoreBlob = append(bl.StoreBlob, service.storeBlob)
	bl.LoadBlob = append(bl.LoadBlob, service.loadBlob)
	bl.DeleteBlob = append(bl.DeleteBlob, service.deleteBlob)

	// Configure upload restrictions
	bl.RejectUpload = append(bl.RejectUpload, service.rejectUpload)

	log.Printf("Blossom service initialized with S3 bucket: %s", cfg.AWSS3BucketName)

	return service, nil
}

// createS3Client creates an AWS S3 client with the provided configuration
func createS3Client(ctx context.Context, cfg *BlossomConfig) (*s3.Client, error) {
	// Create custom credentials provider
	creds := credentials.NewStaticCredentialsProvider(
		cfg.AWSAccessKeyID,
		cfg.AWSSecretKey,
		"",
	)

	// Load AWS config with custom credentials
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(cfg.AWSRegion),
		config.WithCredentialsProvider(creds),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to load AWS config: %w", err)
	}

	// Create S3 client with custom endpoint if provided
	var s3Client *s3.Client
	if cfg.AWSEndpointURL != "" {
		s3Client = s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.AWSEndpointURL)
			o.UsePathStyle = true // Required for most S3-compatible services
		})
	} else {
		s3Client = s3.NewFromConfig(awsCfg)
	}

	return s3Client, nil
}

// storeBlob stores a blob to S3 under the group folder
func (s *BlossomService) storeBlob(ctx context.Context, sha256 string, body []byte) error {
	// Get the group ID from pending uploads
	groupID := ""
	if gid, ok := s.pendingUploads.LoadAndDelete(sha256); ok {
		groupID = gid.(string)
	}

	key := s.buildS3Key(groupID, sha256)

	// Detect content type from the body
	contentType := detectContentType(body)

	_, err := s.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.config.AWSS3BucketName),
		Key:           aws.String(key),
		Body:          bytes.NewReader(body),
		ContentLength: aws.Int64(int64(len(body))),
		ContentType:   aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("failed to store blob to S3: %w", err)
	}

	log.Printf("Stored blob %s to S3 (group: %s)", sha256, groupID)
	return nil
}

// loadBlob loads a blob from S3
// Note: For loading, we need to search for the blob since we don't know the group
func (s *BlossomService) loadBlob(ctx context.Context, sha256 string) (io.ReadSeeker, error) {
	// First try to find the blob by listing possible locations
	// Try the root blobs folder first, then search in group folders
	key, err := s.findBlobKey(ctx, sha256)
	if err != nil {
		return nil, fmt.Errorf("blob not found: %w", err)
	}

	result, err := s.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.config.AWSS3BucketName),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to load blob from S3: %w", err)
	}

	// Read the entire object into memory to return as ReadSeeker
	data, err := io.ReadAll(result.Body)
	result.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to read blob data: %w", err)
	}

	return bytes.NewReader(data), nil
}

// findBlobKey searches for a blob in S3 and returns its key
func (s *BlossomService) findBlobKey(ctx context.Context, sha256 string) (string, error) {
	// Search for any object ending with the sha256 hash
	paginator := s3.NewListObjectsV2Paginator(s.s3Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.config.AWSS3BucketName),
		Prefix: aws.String("blobs/"),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return "", err
		}

		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			// Check if this key ends with our sha256
			if strings.HasSuffix(key, "/"+sha256) || strings.HasSuffix(key, sha256) {
				return key, nil
			}
		}
	}

	return "", fmt.Errorf("blob %s not found", sha256)
}

// deleteBlob deletes a blob from S3
func (s *BlossomService) deleteBlob(ctx context.Context, sha256 string) error {
	// Find the blob first
	key, err := s.findBlobKey(ctx, sha256)
	if err != nil {
		return fmt.Errorf("failed to find blob for deletion: %w", err)
	}

	_, err = s.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.config.AWSS3BucketName),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to delete blob from S3: %w", err)
	}

	log.Printf("Deleted blob %s from S3", sha256)
	return nil
}

// rejectUpload checks if an upload should be rejected
func (s *BlossomService) rejectUpload(ctx context.Context, auth *nostr.Event, size int, ext string) (bool, string, int) {
	// Check file size
	if size > MaxFileSize {
		return true, fmt.Sprintf("file too large, max size is %d MB", MaxFileSize/(1024*1024)), 413
	}

	// Require authentication
	if auth == nil {
		return true, "authentication required", 401
	}

	// Extract the sha256 from the auth event's x tag
	sha256 := auth.Tags.GetFirst([]string{"x", ""})
	if sha256 == nil || len(*sha256) < 2 {
		return true, "missing file hash in auth event", 400
	}

	// Extract the group ID from the auth event's h tag (NIP-29)
	groupTag := auth.Tags.GetFirst([]string{"h", ""})
	groupID := ""
	if groupTag != nil && len(*groupTag) >= 2 {
		groupID = (*groupTag)[1]
	}

	// If a group is specified, verify membership
	if groupID != "" {
		isMember, err := s.isGroupMember(ctx, auth.PubKey, groupID)
		if err != nil {
			log.Printf("Error checking group membership: %v", err)
			return true, "error checking group membership", 500
		}
		if !isMember {
			return true, "not a member of the specified group", 403
		}
	}

	// Store the group ID for use in storeBlob
	s.pendingUploads.Store((*sha256)[1], groupID)

	return false, "", 0
}

// isGroupMember checks if a pubkey is a member of a NIP-29 group
func (s *BlossomService) isGroupMember(ctx context.Context, pubkey string, groupID string) (bool, error) {
	// Query for group membership events
	// In NIP-29, membership can be determined by:
	// 1. Kind 39002 (group members list) events
	// 2. Kind 9000 (add user) events without corresponding 9001 (remove user)

	// First, check for kind 39002 (group members list) which contains all members
	filter := nostr.Filter{
		Kinds: []int{KindGroupMembers},
		Tags:  nostr.TagMap{"d": []string{groupID}},
		Limit: 1,
	}

	events, err := s.eventStore.QueryEvents(ctx, filter)
	if err != nil {
		return false, fmt.Errorf("failed to query group members: %w", err)
	}

	for evt := range events {
		// Check if pubkey is in the p tags
		for _, tag := range evt.Tags {
			if len(tag) >= 2 && tag[0] == "p" && tag[1] == pubkey {
				return true, nil
			}
		}
	}

	// If no members list found, check for add/remove user events
	// Query for the most recent add user event for this pubkey
	addFilter := nostr.Filter{
		Kinds: []int{KindGroupAddUser},
		Tags: nostr.TagMap{
			"h": []string{groupID},
			"p": []string{pubkey},
		},
		Limit: 1,
	}

	addEvents, err := s.eventStore.QueryEvents(ctx, addFilter)
	if err != nil {
		return false, fmt.Errorf("failed to query add user events: %w", err)
	}

	var latestAdd *nostr.Event
	for evt := range addEvents {
		if latestAdd == nil || evt.CreatedAt > latestAdd.CreatedAt {
			latestAdd = evt
		}
	}

	if latestAdd == nil {
		// No add event found, user is not a member
		return false, nil
	}

	// Check if there's a more recent remove event
	removeFilter := nostr.Filter{
		Kinds: []int{KindGroupRemoveUser},
		Tags: nostr.TagMap{
			"h": []string{groupID},
			"p": []string{pubkey},
		},
		Since: &latestAdd.CreatedAt,
		Limit: 1,
	}

	removeEvents, err := s.eventStore.QueryEvents(ctx, removeFilter)
	if err != nil {
		return false, fmt.Errorf("failed to query remove user events: %w", err)
	}

	for range removeEvents {
		// Found a remove event after the add event
		return false, nil
	}

	// User was added and not removed
	return true, nil
}

// buildS3Key constructs the S3 object key from group ID and sha256
func (s *BlossomService) buildS3Key(groupID string, sha256 string) string {
	if groupID != "" {
		// Store under group folder: blobs/{groupID}/{sha256}
		return "blobs/" + groupID + "/" + sha256
	}
	// No group specified, store in root blobs folder
	return "blobs/" + sha256
}

// detectContentType attempts to detect the content type from magic bytes
func detectContentType(data []byte) string {
	if len(data) < 4 {
		return "application/octet-stream"
	}

	// Check magic bytes for common formats
	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	case bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G'}):
		return "image/png"
	case bytes.HasPrefix(data, []byte{'G', 'I', 'F', '8'}):
		return "image/gif"
	case bytes.HasPrefix(data, []byte{'R', 'I', 'F', 'F'}) && len(data) > 8 && string(data[8:12]) == "WEBP":
		return "image/webp"
	case bytes.HasPrefix(data, []byte{0x00, 0x00, 0x00}) && len(data) > 4 && (data[4] == 0x18 || data[4] == 0x20):
		return "video/mp4"
	case bytes.HasPrefix(data, []byte{0x1A, 0x45, 0xDF, 0xA3}):
		return "video/webm"
	case bytes.HasPrefix(data, []byte{'I', 'D', '3'}) || bytes.HasPrefix(data, []byte{0xFF, 0xFB}):
		return "audio/mpeg"
	case bytes.HasPrefix(data, []byte{'O', 'g', 'g', 'S'}):
		return "audio/ogg"
	case bytes.HasPrefix(data, []byte{'R', 'I', 'F', 'F'}) && len(data) > 8 && string(data[8:12]) == "WAVE":
		return "audio/wav"
	case bytes.HasPrefix(data, []byte{'%', 'P', 'D', 'F'}):
		return "application/pdf"
	case bytes.HasPrefix(data, []byte{'{'}) || bytes.HasPrefix(data, []byte{'['}):
		return "application/json"
	case isText(data):
		return "text/plain"
	case strings.HasPrefix(string(data), "<svg"):
		return "image/svg+xml"
	}

	return "application/octet-stream"
}

// isText checks if data appears to be text
func isText(data []byte) bool {
	for _, b := range data[:min(512, len(data))] {
		if b < 32 && b != '\n' && b != '\r' && b != '\t' {
			return false
		}
	}
	return true
}

// Blossom returns the underlying blossom server for additional configuration
func (s *BlossomService) Blossom() *blossom.BlossomServer {
	return s.blossom
}
