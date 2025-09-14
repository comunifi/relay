package relay

import (
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

func TestEvent_ParseEventSignature(t *testing.T) {
	tests := []struct {
		name          string
		signature     string
		wantEventName string
		wantArgNames  []string
		wantArgTypes  []ArgType
	}{
		{
			name:          "Full signature with named arguments and spaces",
			signature:     "Transfer(address from, address to, uint256 value)",
			wantEventName: "Transfer",
			wantArgNames:  []string{"from", "to", "value"},
			wantArgTypes:  []ArgType{{Name: "address", Indexed: false}, {Name: "address", Indexed: false}, {Name: "uint256", Indexed: false}},
		},
		{
			name:          "Full signature with named arguments",
			signature:     "Transfer(address from,address to,uint256 value)",
			wantEventName: "Transfer",
			wantArgNames:  []string{"from", "to", "value"},
			wantArgTypes:  []ArgType{{Name: "address", Indexed: false}, {Name: "address", Indexed: false}, {Name: "uint256", Indexed: false}},
		},
		{
			name:          "Full signature with named indexed arguments",
			signature:     "Transfer(address indexed from,address indexed to,uint256 value)",
			wantEventName: "Transfer",
			wantArgNames:  []string{"from", "to", "value"},
			wantArgTypes:  []ArgType{{Name: "address", Indexed: true}, {Name: "address", Indexed: true}, {Name: "uint256", Indexed: false}},
		},
		{
			name:          "Full signature with named indexed arguments",
			signature:     "Transfer (index_topic_1 address from, index_topic_2 address to, uint256 value)",
			wantEventName: "Transfer",
			wantArgNames:  []string{"from", "to", "value"},
			wantArgTypes:  []ArgType{{Name: "address", Indexed: true}, {Name: "address", Indexed: true}, {Name: "uint256", Indexed: false}},
		},
		{
			name:          "Compact signature without named arguments",
			signature:     "Transfer(address,address,uint256)",
			wantEventName: "Transfer",
			wantArgNames:  []string{"0", "1", "2"},
			wantArgTypes:  []ArgType{{Name: "address", Indexed: false}, {Name: "address", Indexed: false}, {Name: "uint256", Indexed: false}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Event{EventSignature: tt.signature}
			gotEventName, gotArgNames, gotArgTypes := e.ParseEventSignature()

			if gotEventName != tt.wantEventName {
				t.Errorf("Event.ParseEventSignature() eventName = %v, want %v", gotEventName, tt.wantEventName)
			}

			if !reflect.DeepEqual(gotArgNames, tt.wantArgNames) {
				t.Errorf("Event.ParseEventSignature() argNames = %v, want %v", gotArgNames, tt.wantArgNames)
			}

			if !reflect.DeepEqual(gotArgTypes, tt.wantArgTypes) {
				t.Errorf("Event.ParseEventSignature() argTypes = %v, want %v", gotArgTypes, tt.wantArgTypes)
			}
		})
	}
}

func TestEvent_ParseIndexedEventSignature(t *testing.T) {
	tests := []struct {
		name          string
		signature     string
		wantEventName string
		wantArgNames  []string
		wantArgTypes  []ArgType
	}{
		{
			name:          "Full signature with named arguments and spaces",
			signature:     "Transfer(address indexed from, address indexed to, uint256 value)",
			wantEventName: "Transfer",
			wantArgNames:  []string{"from", "to", "value"},
			wantArgTypes:  []ArgType{{Name: "address", Indexed: true}, {Name: "address", Indexed: true}, {Name: "uint256", Indexed: false}},
		},
		{
			name:          "Full signature with named arguments",
			signature:     "Transfer(address indexed from,address indexed to,uint256 value)",
			wantEventName: "Transfer",
			wantArgNames:  []string{"from", "to", "value"},
			wantArgTypes:  []ArgType{{Name: "address", Indexed: true}, {Name: "address", Indexed: true}, {Name: "uint256", Indexed: false}},
		},
		{
			name:          "Compact signature without named arguments",
			signature:     "Transfer(address indexed,address indexed,uint256)",
			wantEventName: "Transfer",
			wantArgNames:  []string{"0", "1", "2"},
			wantArgTypes:  []ArgType{{Name: "address", Indexed: true}, {Name: "address", Indexed: true}, {Name: "uint256", Indexed: false}},
		},
		{
			name:          "Compact signature without named arguments",
			signature:     "Transfer(address,address indexed,uint256 indexed)",
			wantEventName: "Transfer",
			wantArgNames:  []string{"0", "1", "2"},
			wantArgTypes:  []ArgType{{Name: "address", Indexed: false}, {Name: "address", Indexed: true}, {Name: "uint256", Indexed: true}},
		},
		{
			name:          "Compact signature without named arguments",
			signature:     "Transfer(uint256,address indexed,address indexed)",
			wantEventName: "Transfer",
			wantArgNames:  []string{"0", "1", "2"},
			wantArgTypes:  []ArgType{{Name: "uint256", Indexed: false}, {Name: "address", Indexed: true}, {Name: "address", Indexed: true}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Event{EventSignature: tt.signature}
			gotEventName, gotArgNames, gotArgTypes := e.ParseEventSignature()

			if gotEventName != tt.wantEventName {
				t.Errorf("Event.ParseEventSignature() eventName = %v, want %v", gotEventName, tt.wantEventName)
			}

			if !reflect.DeepEqual(gotArgNames, tt.wantArgNames) {
				t.Errorf("Event.ParseEventSignature() argNames = %v, want %v", gotArgNames, tt.wantArgNames)
			}

			if !reflect.DeepEqual(gotArgTypes, tt.wantArgTypes) {
				t.Errorf("Event.ParseEventSignature() argTypes = %v, want %v", gotArgTypes, tt.wantArgTypes)
			}
		})
	}
}

func TestGetTopic0FromEventSignature(t *testing.T) {
	testCases := []struct {
		name           string
		eventSignature string
		expectedTopic0 string
	}{
		{
			name:           "Transfer event",
			eventSignature: "Transfer(address,address,uint256)",
			expectedTopic0: "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",
		},
		{
			name:           "Approval event",
			eventSignature: "Approval(address,address,uint256)",
			expectedTopic0: "0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925",
		},
		{
			name:           "Named arguments",
			eventSignature: "Transfer(address from, address to, uint256 value)",
			expectedTopic0: "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",
		},
		{
			name:           "Named indexed arguments",
			eventSignature: "Transfer(address indexed from, address indexed to, uint256 value)",
			expectedTopic0: "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",
		},
		{
			name:           "Named indexed arguments",
			eventSignature: "Transfer (index_topic_1 address from, index_topic_2 address to, uint256 value)",
			expectedTopic0: "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",
		},
		{
			name:           "Empty signature",
			eventSignature: "",
			expectedTopic0: "0x0000000000000000000000000000000000000000000000000000000000000000",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			event := &Event{EventSignature: tc.eventSignature}
			topic0 := event.GetTopic0FromEventSignature()
			expectedTopic0 := common.HexToHash(tc.expectedTopic0)
			assert.Equal(t, expectedTopic0, topic0, "Topic0 mismatch for event signature: %s", tc.eventSignature)
		})
	}
}

func TestConstructABIFromEventSignature(t *testing.T) {
	tests := []struct {
		name           string
		eventSignature string
		expectedABI    string
		expectError    bool
	}{
		{
			name:           "Simple event",
			eventSignature: "Transfer(address from, address to, uint256 value)",
			expectedABI:    `[{"name":"Transfer","type":"event","inputs":[{"name":"from","type":"address","indexed":false},{"name":"to","type":"address","indexed":false},{"name":"value","type":"uint256","indexed":false}]}]`,
			expectError:    false,
		},
		{
			name:           "Event with indexed parameters",
			eventSignature: "Transfer(address indexed from, address indexed to, uint256 value)",
			expectedABI:    `[{"name":"Transfer","type":"event","inputs":[{"name":"from","type":"address","indexed":true},{"name":"to","type":"address","indexed":true},{"name":"value","type":"uint256","indexed":false}]}]`,
			expectError:    false,
		},
		{
			name:           "Event with indexed parameters",
			eventSignature: "Transfer (index_topic_1 address from, index_topic_2 address to, uint256 value)",
			expectedABI:    `[{"name":"Transfer","type":"event","inputs":[{"name":"from","type":"address","indexed":true},{"name":"to","type":"address","indexed":true},{"name":"value","type":"uint256","indexed":false}]}]`,
			expectError:    false,
		},
		{
			name:           "Event with unnamed parameters",
			eventSignature: "Transfer(address,address,uint256)",
			expectedABI:    `[{"name":"Transfer","type":"event","inputs":[{"name":"0","type":"address","indexed":false},{"name":"1","type":"address","indexed":false},{"name":"2","type":"uint256","indexed":false}]}]`,
			expectError:    false,
		},
		{
			name:           "Empty event signature",
			eventSignature: "",
			expectedABI:    "",
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &Event{EventSignature: tt.eventSignature}
			abi, err := event.ConstructABIFromEventSignature()

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.JSONEq(t, tt.expectedABI, abi)
			}
		})
	}
}

func TestEvent_ConstructABIFromEventSignature_EmptySignature(t *testing.T) {
	tests := []struct {
		name           string
		eventSignature string
		expectError    bool
	}{
		{
			name:           "Empty event signature",
			eventSignature: "",
			expectError:    true,
		},
		{
			name:           "Malformed event signature - missing parentheses",
			eventSignature: "Transfer",
			expectError:    true,
		},
		{
			name:           "Malformed event signature - empty arguments",
			eventSignature: "Transfer()",
			expectError:    true,
		},
		{
			name:           "Valid event signature",
			eventSignature: "Transfer(address from, address to, uint256 value)",
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &Event{EventSignature: tt.eventSignature}
			abi, err := event.ConstructABIFromEventSignature()

			if tt.expectError {
				assert.Error(t, err)
				assert.Empty(t, abi)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, abi)
			}
		})
	}
}

func TestEvent_IsValidData(t *testing.T) {
	tests := []struct {
		name           string
		eventSignature string
		data           map[string]interface{}
		want           bool
	}{
		{
			name:           "Valid data with named arguments",
			eventSignature: "Transfer(address from, address to, uint256 value)",
			data: map[string]interface{}{
				"topic": "0x...",
				"from":  "0x1234...",
				"to":    "0x5678...",
				"value": "1000000000000000000",
			},
			want: true,
		},
		{
			name:           "Valid data with unnamed arguments",
			eventSignature: "Transfer(address,address,uint256)",
			data: map[string]interface{}{
				"topic": "0x...",
				"0":     "0x1234...",
				"1":     "0x5678...",
				"2":     "1000000000000000000",
			},
			want: true,
		},
		{
			name:           "Invalid data - missing topic",
			eventSignature: "Transfer(address from, address to, uint256 value)",
			data: map[string]interface{}{
				"from":  "0x1234...",
				"to":    "0x5678...",
				"value": "1000000000000000000",
			},
			want: false,
		},
		{
			name:           "Invalid data - extra field",
			eventSignature: "Transfer(address from, address to, uint256 value)",
			data: map[string]interface{}{
				"topic": "0x...",
				"from":  "0x1234...",
				"to":    "0x5678...",
				"value": "1000000000000000000",
				"extra": "extra field",
			},
			want: false,
		},
		{
			name:           "Invalid data - missing field",
			eventSignature: "Transfer(address from, address to, uint256 value)",
			data: map[string]interface{}{
				"topic": "0x...",
				"from":  "0x1234...",
				"to":    "0x5678...",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Event{EventSignature: tt.eventSignature}
			if got := e.IsValidData(tt.data); got != tt.want {
				t.Errorf("Event.IsValidData() = %v, want %v", got, tt.want)
			}
		})
	}
}
