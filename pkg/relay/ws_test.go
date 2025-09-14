package relay

import (
	"encoding/json"
	"testing"
)

func TestLogMatchesQuery(t *testing.T) {
	testCases := []struct {
		name     string
		logData  map[string]any
		query    string
		expected bool
	}{
		{
			name:     "Empty query",
			logData:  map[string]any{"field": "value"},
			query:    "",
			expected: true,
		},
		{
			name:     "Matching single field",
			logData:  map[string]any{"field": "value"},
			query:    "data.field=value",
			expected: true,
		},
		{
			name:     "Non-matching single field",
			logData:  map[string]any{"field": "value"},
			query:    "data.field=wrong",
			expected: false,
		},
		{
			name:     "Matching one of multiple fields",
			logData:  map[string]any{"field1": "value1", "field2": "value2"},
			query:    "data.field1=value1&data.field2=wrong",
			expected: true,
		},
		{
			name:     "Non-matching multiple fields",
			logData:  map[string]any{"field1": "value1", "field2": "value2"},
			query:    "data.field1=wrong&data.field2=wrong",
			expected: false,
		},
		{
			name:     "Ignore non-data fields",
			logData:  map[string]any{"field": "value"},
			query:    "other=value",
			expected: false,
		},
		{
			name:     "Match integer value",
			logData:  map[string]any{"count": 42},
			query:    "data.count=42",
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			jsonData, err := json.Marshal(tc.logData)
			if err != nil {
				t.Fatalf("Failed to marshal test data: %v", err)
			}

			log := &Log{
				Data: (*json.RawMessage)(&jsonData),
			}

			result := log.MatchesQuery(tc.query)
			if result != tc.expected {
				t.Errorf("Expected MatchesQuery to return %v, but got %v", tc.expected, result)
			}
		})
	}
}
