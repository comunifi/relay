package relay

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

type Topic struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

type Topics []Topic

func ParseTopicsFromHashes(event *Event, topicHashes []common.Hash, data []byte) (Topics, error) {
	if event == nil {
		return nil, fmt.Errorf("event is required")
	}

	if len(topicHashes) == 0 {
		return nil, fmt.Errorf("no topic hashes provided")
	}

	name, args, argTypes := event.ParseEventSignature()
	if name == "" || len(args) == 0 || len(argTypes) == 0 {
		return nil, fmt.Errorf("event name is required")
	}

	topics := Topics{}

	// First topic is always the event signature hash
	topics = append(topics, Topic{
		Name:  "topic",
		Type:  "bytes32",
		Value: topicHashes[0],
	})

	rawEventABI, err := event.ConstructABIFromEventSignature()
	if err != nil {
		return nil, err
	}

	// Check if the ABI string is empty
	if rawEventABI == "" {
		return nil, fmt.Errorf("event signature is empty or invalid")
	}

	eventABI, err := abi.JSON(strings.NewReader(rawEventABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ABI from event signature: %w", err)
	}

	// Create a new ABI with only non-indexed inputs
	nonIndexedInputs := abi.Arguments{}
	for _, input := range eventABI.Events[name].Inputs {
		if !input.Indexed {
			nonIndexedInputs = append(nonIndexedInputs, input)
		}
	}

	unpacked := &map[string]any{}

	// Unpack only non-indexed parameters
	err = abi.Arguments(nonIndexedInputs).UnpackIntoMap(*unpacked, data)
	if err != nil {
		return nil, err
	}

	indexedTopicIndex := 1
	// Parse remaining topics
	for i, argType := range argTypes {
		t := Topic{
			Name: args[i],
			Type: argType.Name,
		}

		if argType.Indexed {
			err := t.convertHashToValue(topicHashes[indexedTopicIndex])
			if err != nil {
				return nil, err
			}

			topics = append(topics, t)

			indexedTopicIndex++

			continue
		}

		t.Value = (*unpacked)[args[i]]

		topics = append(topics, t)
	}

	return topics, nil
}

func (t *Topics) String() string {
	ts := make([]string, len(*t))
	for i, topic := range *t {
		ts[i] = fmt.Sprintf("%s: %s", topic.Name, topic.Value)
	}

	return strings.Join(ts, ", ")
}

func (t Topics) MarshalJSON() ([]byte, error) {
	m := map[string]any{}

	for _, topic := range t {
		if topic.Name == "" {
			continue
		}
		m[topic.Name] = topic.valueToJsonParseable()
	}

	return json.Marshal(m)
}

func (t *Topic) valueToJsonParseable() any {
	switch v := t.Value.(type) {
	case bool, string:
		return v
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return v
	case *big.Int:
		return v.String()
	case []byte:
		return "0x" + common.Bytes2Hex(v)
	case common.Address:
		return v.Hex()
	case common.Hash:
		return v.Hex()
	default:
		return v
	}
}

func (t *Topic) convertHashToValue(hash common.Hash) error {
	bytes := hash.Bytes()

	switch t.Type {
	case "bool":
		t.Value = bytes[31] != 0
		return nil
	case "address":
		t.Value = common.HexToAddress(hash.Hex())
		return nil
	case "string", "bytes":
		// For dynamic types, the hash is actually a pointer to the data
		// In this case, we can't retrieve the actual value from just the hash
		t.Value = hash.Hex()
		return nil
	default:
		// Handle integer types
		if strings.HasPrefix(t.Type, "uint") || strings.HasPrefix(t.Type, "int") {
			bitSize, err := strconv.Atoi(strings.TrimPrefix(strings.TrimPrefix(t.Type, "uint"), "int"))
			if err != nil {
				bitSize = 256 // Default to 256 if no size specified
			}

			value := new(big.Int).SetBytes(bytes)

			if strings.HasPrefix(t.Type, "int") && bitSize < 256 {
				// Handle sign extension for smaller signed integers
				if value.Bit(bitSize-1) == 1 {
					mask := new(big.Int).Lsh(big.NewInt(1), uint(bitSize))
					mask.Sub(mask, big.NewInt(1))
					value.And(value, mask)
					value.Neg(value)
				}
			}

			t.Value = value
			return nil
		}

		// Handle fixed-size byte arrays
		if strings.HasPrefix(t.Type, "bytes") {
			size, err := strconv.Atoi(strings.TrimPrefix(t.Type, "bytes"))
			if err != nil {
				return fmt.Errorf("invalid bytes type: %s", t.Type)
			}
			t.Value = bytes[:size]
			return nil
		}
	}

	return fmt.Errorf("unsupported type: %s", t.Type)
}

func (t Topics) Value() (driver.Value, error) {
	jsonData, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	return jsonData, nil
}

func (t *Topics) GenerateTopicQuery(start int) (string, []any) {
	topicQuery := `
		`
	args := []any{}
	for _, topic := range *t {
		topicQuery += fmt.Sprintf("data->>'%s' = $%d AND ", topic.Name, start)
		args = append(args, topic.Value)
		start++
	}
	topicQuery += `
		`
	return topicQuery, args
}

func ParseJSONBFilters(query url.Values, prefix string) map[string]any {
	jsonFilter := make(map[string]any)

	for key, values := range query {
		if strings.HasPrefix(key, prefix+".") && len(values) > 0 {
			parts := strings.SplitN(key, ".", 2)
			if len(parts) == 2 {
				jsonFilter[parts[1]] = values[0]
			}
		}
	}

	return jsonFilter
}

func GenerateJSONBQuery(prefix string, start int, data map[string]any) (string, []any) {
	var query strings.Builder
	args := make([]any, 0, len(data))

	i := start
	for key, value := range data {
		if i > start {
			query.WriteString(" AND ")
		}
		query.WriteString(fmt.Sprintf("%sdata->>'%s' = $%d", prefix, key, i))
		args = append(args, value)
		i++
	}

	return query.String(), args
}
