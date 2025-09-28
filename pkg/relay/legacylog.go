package relay

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type LegacyLogStatus string

const (
	LegacyLogStatusUnknown LegacyLogStatus = ""
	LegacyLogStatusSending LegacyLogStatus = "sending"
	LegacyLogStatusPending LegacyLogStatus = "pending"
	LegacyLogStatusSuccess LegacyLogStatus = "success"
	LegacyLogStatusFail    LegacyLogStatus = "fail"

	TEMP_HASH_PREFIX = "TEMP_HASH"
)

func LegacyLogStatusFromString(s string) (LegacyLogStatus, error) {
	switch s {
	case "sending":
		return LegacyLogStatusSending, nil
	case "pending":
		return LegacyLogStatusPending, nil
	case "success":
		return LegacyLogStatusSuccess, nil
	case "fail":
		return LegacyLogStatusFail, nil
	}

	return LegacyLogStatusUnknown, errors.New("unknown role: " + s)
}

type LegacyLog struct {
	Hash      string           `json:"hash"`
	TxHash    string           `json:"tx_hash"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
	Nonce     int64            `json:"nonce"`
	Sender    string           `json:"sender"`
	To        string           `json:"to"`
	Value     *big.Int         `json:"value"`
	Data      *json.RawMessage `json:"data"`
	ExtraData *json.RawMessage `json:"extra_data"`
	Status    LegacyLogStatus  `json:"status"`
}

type ExtraData struct {
	Description string `json:"description"`
}

// generate hash for transfer using a provided index, from, to and the tx hash
func (t *LegacyLog) GenerateUniqueHash(chainID string) string {
	buf := new(bytes.Buffer)

	// Write each value to the buffer as bytes
	// Convert t.Value to a fixed-length byte representation
	valueBytes := t.Value.Bytes()
	buf.Write(common.LeftPadBytes(valueBytes, 32))
	if t.Data != nil {
		buf.Write(sortedJSONBytes(t.Data))
	}

	buf.Write(common.FromHex(t.TxHash))
	buf.Write(common.FromHex(chainID))

	hash := crypto.Keccak256Hash(buf.Bytes())
	return hash.Hex()
}

func (t *LegacyLog) ToRounded(decimals int64) float64 {
	v, _ := t.Value.Float64()

	if decimals == 0 {
		return v
	}

	// Calculate value * 10^x
	multiplier, _ := new(big.Int).Exp(big.NewInt(10), big.NewInt(decimals), nil).Float64()

	result, _ := new(big.Float).Quo(big.NewFloat(v), big.NewFloat(multiplier)).Float64()

	return result
}

// Update updates the transfer using the given transfer
func (t *LegacyLog) Update(tx *LegacyLog) {
	// update all fields
	t.Hash = tx.Hash
	t.TxHash = tx.TxHash
	t.CreatedAt = tx.CreatedAt
	t.UpdatedAt = time.Now()
	t.Nonce = tx.Nonce
	t.Sender = tx.Sender
	t.To = tx.To
	t.Value = tx.Value
	t.Data = tx.Data
	t.ExtraData = tx.ExtraData
	t.Status = tx.Status
}

func (t *LegacyLog) GetPoolTopic() *string {
	if t.Data == nil {
		return nil
	}

	var data map[string]any

	json.Unmarshal(*t.Data, &data)

	v, ok := data["topic"].(string)
	if !ok {
		return nil
	}

	topic := strings.ToLower(fmt.Sprintf("%s/%s", t.To, v))

	return &topic
}

// Convert a log to json bytes
func (t *LegacyLog) ToJSON() []byte {
	b, err := json.Marshal(t)
	if err != nil {
		return nil
	}

	return b
}

func sortedJSONBytes(data *json.RawMessage) []byte {
	if data == nil {
		return nil
	}

	var m map[string]interface{}
	if err := json.Unmarshal(*data, &m); err != nil {
		// If it's not a JSON object, return the raw bytes
		return *data
	}

	// Get sorted keys
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build sorted buffer
	var buf bytes.Buffer
	for _, k := range keys {
		v := m[k]
		keyBytes, _ := json.Marshal(k)
		valueBytes, _ := json.Marshal(v)
		buf.Write(keyBytes)
		buf.Write(valueBytes)
	}

	return buf.Bytes()
}
