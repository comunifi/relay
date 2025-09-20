package relay

import (
	nostreth "github.com/comunifi/nostr-eth"
)

type WSMessageType string

const (
	WSMessageTypeNew    WSMessageType = "new"
	WSMessageTypeUpdate WSMessageType = "update"
	WSMessageTypeRemove WSMessageType = "remove"
)

type WSMessageDataType string

const (
	WSMessageDataTypeLog WSMessageDataType = "log"
)

type WSMessage struct {
	PoolID string        `json:"pool_id"`
	Type   WSMessageType `json:"type"`
	ID     string        `json:"id"`
}

type WSMessageLog struct {
	WSMessage
	DataType WSMessageDataType `json:"data_type"`
	Data     nostreth.Log      `json:"data"`
}

type WSMessageCreator interface {
	ToWSMessage(t WSMessageType) *WSMessageLog
	MatchesQuery(query string) bool
}

// func (l *nostreth.Log) ToWSMessage(t WSMessageType) *WSMessageLog {
// 	poolTopic := l.GetPoolTopic()
// 	if poolTopic == nil {
// 		return nil
// 	}

// 	b := l.ToJSON()
// 	if b == nil {
// 		return nil
// 	}

// 	return &WSMessageLog{
// 		WSMessage: WSMessage{
// 			PoolID: *poolTopic,
// 			Type:   t,
// 			ID:     l.Hash,
// 		},
// 		DataType: WSMessageDataTypeLog,
// 		Data:     *l,
// 	}
// }

// func (l *Log) MatchesQuery(query string) bool {
// 	// Empty query matches everything
// 	if query == "" {
// 		return true
// 	}

// 	var data map[string]any
// 	err := json.Unmarshal(*l.Data, &data)
// 	if err != nil {
// 		return false
// 	}

// 	// Parse the query string
// 	params := strings.Split(query, "&")
// 	for _, param := range params {
// 		kv := strings.SplitN(param, "=", 2)
// 		if len(kv) != 2 {
// 			continue
// 		}
// 		key, value := kv[0], kv[1]

// 		// Check if the key starts with "data."
// 		if strings.HasPrefix(key, "data.") {
// 			dataField := strings.TrimPrefix(key, "data.")
// 			if dataValue, ok := data[dataField]; ok {
// 				if fmt.Sprintf("%v", dataValue) == value {
// 					return true
// 				}
// 			}
// 		}
// 	}

// 	return false
// }
