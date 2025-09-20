package common

import (
	"context"

	"github.com/comunifi/relay/pkg/relay"
)

// GetContextAddress returns the indexer.ContextKeyAddress from the context
func GetContextAddress(ctx context.Context) (string, bool) {
	addr, ok := ctx.Value(relay.ContextKeyAddress).(string)
	return addr, ok
}
