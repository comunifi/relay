package nostr

import (
	"regexp"
)

func removeNostrUris(content string) string {
	// Regular expression to match nostr URIs
	// This matches "nostr:" followed by any characters that are valid in nostr URIs
	nostrUriRegex := regexp.MustCompile(`nostr:[a-zA-Z0-9]+`)

	// Replace all nostr URIs with empty string
	return nostrUriRegex.ReplaceAllString(content, "")
}
