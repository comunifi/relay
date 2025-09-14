package relay

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLog_GenerateUniqueHash(t *testing.T) {
	d := json.RawMessage(`{"method":"transfer"}`)

	// Create a sample Log instance
	log := &Log{
		To:     "0x742d35Cc6634C0532925a3b844Bc454e4438f44e",
		Value:  big.NewInt(1000000000000000000), // 1 ETH
		Data:   &d,
		TxHash: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
	}

	// Generate the unique hash
	hash := log.GenerateUniqueHash()

	// Assert that the hash is not empty
	assert.NotEmpty(t, hash)

	// Assert that the hash is 66 characters long (0x + 64 hex characters)
	assert.Len(t, hash, 66)

	// Assert that the hash starts with "0x"
	assert.True(t, hash[:2] == "0x")

	// Generate the hash again and assert that it's the same
	hash2 := log.GenerateUniqueHash()
	assert.Equal(t, hash, hash2)

	// Modify a field and assert that the hash changes
	log.Value = big.NewInt(2000000000000000000) // 2 ETH
	hash3 := log.GenerateUniqueHash()
	assert.NotEqual(t, hash, hash3)
}
