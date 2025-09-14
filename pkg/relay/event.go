package relay

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type Event struct {
	Contract       string    `json:"contract"`
	EventSignature string    `json:"event_signature"`
	Name           string    `json:"name"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type ArgType struct {
	Name    string
	Indexed bool
}

// Parse human readable event signature
// Example: Transfer(address from, address to, uint256 value)
// Returns: ("Transfer", ["from", "to", "value"], [{Name: "address", Indexed: false}, {Name: "address", Indexed: false}, {Name: "uint256", Indexed: false}])
//
// Example: Transfer(address,address,uint256)
// Returns: ("Transfer", ["0", "1", "2"], [{Name: "address", Indexed: false}, {Name: "address", Indexed: false}, {Name: "uint256", Indexed: false}])
//
// Example: Transfer(address indexed from, address indexed to, uint256 value)
// Returns: ("Transfer", ["from", "to", "value"], [{Name: "address", Indexed: true}, {Name: "address", Indexed: true}, {Name: "uint256", Indexed: false}])
//
// Example: Transfer(address indexed, address indexed, uint256)
// Returns: ("Transfer", ["0", "1", "2"], [{Name: "address", Indexed: true}, {Name: "address", Indexed: true}, {Name: "uint256", Indexed: false}])
//
// Example: Transfer (index_topic_1 address from, index_topic_2 address to, uint256 value)
// Returns: ("Transfer", ["from", "to", "value"], [{Name: "address", Indexed: true}, {Name: "address", Indexed: true}, {Name: "uint256", Indexed: false}])
func (e *Event) ParseEventSignature() (string, []string, []ArgType) {
	if e.EventSignature == "" {
		return "", []string{}, []ArgType{}
	}

	parts := strings.SplitN(e.EventSignature, "(", 2)
	if len(parts) != 2 {
		return "", []string{}, []ArgType{}
	}

	eventName := strings.TrimSpace(parts[0])
	if eventName == "" {
		return "", []string{}, []ArgType{}
	}

	argNames := []string{}
	argTypes := []ArgType{}

	rawArgs := strings.TrimSuffix(parts[1], ")")
	argParts := strings.Split(rawArgs, ",")

	for i, arg := range argParts {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue // Skip empty arguments
		}

		parts := strings.Fields(arg)
		if len(parts) == 0 {
			continue // Skip arguments with no fields
		}

		isIndexed := false
		var argName, argType string

		// Check for "index_topic_N" format
		if strings.HasPrefix(parts[0], "index_topic_") {
			isIndexed = true
			parts = parts[1:] // Remove "index_topic_N" from parts
		}

		if len(parts) >= 2 && (parts[0] == "indexed" || parts[1] == "indexed") {
			// Indexed argument
			isIndexed = true
			if parts[0] == "indexed" {
				parts = parts[1:] // Remove "indexed" from the beginning
			} else {
				parts = append(parts[:1], parts[2:]...) // Remove "indexed" from the middle
			}
		}

		if len(parts) == 2 {
			// Named argument
			argName = parts[1]
			argType = parts[0]
		} else if len(parts) == 1 {
			// Unnamed argument
			argName = strconv.Itoa(i)
			argType = parts[0]
		} else {
			// Invalid argument format, skip it
			continue
		}

		// Only add valid arguments
		if argType != "" {
			argNames = append(argNames, argName)
			argTypes = append(argTypes, ArgType{Name: argType, Indexed: isIndexed})
		}
	}

	return eventName, argNames, argTypes
}

func (e *Event) GetTopic0FromEventSignature() common.Hash {
	name, _, argTypes := e.ParseEventSignature()
	if name == "" || len(argTypes) == 0 {
		return common.Hash{}
	}

	types := make([]string, len(argTypes))
	for i, argType := range argTypes {
		types[i] = argType.Name
	}

	funcSig := fmt.Sprintf("%s(%s)", name, strings.Join(types, ","))

	return crypto.Keccak256Hash([]byte(funcSig))
}

// ConstructABIFromEventSignature constructs an ABI from an event signature
// Example: Transfer(from address, to address, value uint256)
// Returns: {"name":"Transfer","type":"event","inputs":[{"name":"from","type":"address","indexed":false},{"name":"to","type":"address","indexed":false},{"name":"value","type":"uint256","indexed":false}]}
//
// Example: Transfer(from indexed address, to indexed address, value uint256)
// Returns: {"name":"Transfer","type":"event","inputs":[{"name":"from","type":"address", "indexed": true},{"name":"to","type":"address", "indexed": true},{"name":"value","type":"uint256", "indexed": false}]}
func (e *Event) ConstructABIFromEventSignature() (string, error) {
	name, args, argTypes := e.ParseEventSignature()
	if name == "" || len(args) == 0 || len(argTypes) == 0 {
		return "", fmt.Errorf("event name is required")
	}

	// Validate that all argument types are non-empty
	for i, argType := range argTypes {
		if argType.Name == "" {
			return "", fmt.Errorf("argument type at index %d is empty", i)
		}
	}

	abi := fmt.Sprintf(`[{"name":"%s","type":"event","inputs":[`, name)
	for i, arg := range args {
		abi += fmt.Sprintf(`{"name":"%s","type":"%s","indexed":%t}`, arg, argTypes[i].Name, argTypes[i].Indexed)

		// add comma if not last argument
		if i < len(args)-1 {
			abi += ","
		}
	}
	abi += `]}]`

	return abi, nil
}

// IsValidData checks if the provided data contains exactly all the argument names
// returned by ParseEventSignature, plus the "topic" field, no more and no less.
func (e *Event) IsValidData(data map[string]any) bool {
	_, argNames, _ := e.ParseEventSignature()

	// Check if the number of keys in data matches the number of argument names plus one (for "topic")
	if len(data) != len(argNames)+1 {
		return false
	}

	// Check if "topic" is present in the data
	if _, exists := data["topic"]; !exists {
		return false
	}

	// Check if all argument names are present in the data
	for _, argName := range argNames {
		if _, exists := data[argName]; !exists {
			return false
		}
	}

	return true
}
