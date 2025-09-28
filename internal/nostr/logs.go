package nostr

import (
	"encoding/json"
	"strings"
	"time"

	nostreth "github.com/comunifi/nostr-eth"
	"github.com/comunifi/relay/pkg/relay"
	"github.com/jackc/pgx/v5"
	"github.com/lib/pq"
)

// GetLog returns the log for a given hash by querying the "d" tag
func (n *Nostr) GetLog(hash, chainID string) (*relay.LegacyLog, error) {
	var log relay.LegacyLog

	// Collect unique values for tagvalues query
	tagValues := []string{chainID, hash}

	// Query the event table for events using tagvalues @> approach
	row := n.ndb.QueryRow(`
		SELECT id, pubkey, created_at, kind, content, sig, tags
		FROM event
		WHERE kind = $1 
		AND tagvalues @> $2
		LIMIT 1
	`, nostreth.KindTxLog, pq.Array(tagValues))

	var id, pubkey, content, sig string
	var createdAt int64
	var kind int
	var tags json.RawMessage

	err := row.Scan(&id, &pubkey, &createdAt, &kind, &content, &sig, &tags)
	if err != nil {
		return nil, err
	}

	var nlog nostreth.TxLogEvent

	err = json.Unmarshal([]byte(content), &nlog)
	if err != nil {
		return nil, err
	}

	// standard properties
	log.Hash = nlog.LogData.Hash
	log.TxHash = nlog.LogData.TxHash
	log.CreatedAt = nlog.LogData.CreatedAt
	log.UpdatedAt = nlog.LogData.UpdatedAt
	log.Nonce = nlog.LogData.Nonce
	log.Sender = nlog.LogData.Sender
	log.To = nlog.LogData.To
	log.Value = nlog.LogData.Value
	log.Data = nlog.LogData.Data

	// hard coded because we stopped doing optimistic indexing
	log.Status = relay.LegacyLogStatusSuccess

	// v1 requires the message as extra data, attempt to find a message
	mentionEvent, err := n.GetMentionEvent(id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return &log, nil
		}

		return nil, err
	}

	extraData := &relay.ExtraData{
		Description: mentionEvent.Content,
	}

	var extraDataJSON json.RawMessage

	extraDataJSON, err = json.Marshal(extraData)
	if err != nil {
		return nil, err
	}

	log.ExtraData = &extraDataJSON

	return &log, nil
}

// GetAllPaginatedLogs returns the logs paginated
func (n *Nostr) GetAllPaginatedLogs(contract string, topic string, maxDate time.Time, limit, offset int) ([]*relay.LegacyLog, error) {
	logs := []*relay.LegacyLog{}

	// Collect unique values for tagvalues query
	tagValues := []string{contract}

	// Query the event table for tx_log events with pagination and filtering
	query := `
		SELECT id, pubkey, created_at, kind, content, sig, tags
		FROM event
		WHERE kind = $1 
		AND created_at <= $2
		AND tagvalues @> $3
		AND EXISTS (
			SELECT 1
			FROM jsonb_array_elements(tags) AS tag
			WHERE tag->>0 = 'topic' AND tag->>1 = $4
		)
		ORDER BY created_at DESC
		LIMIT $5 OFFSET $6
	`

	args := []any{nostreth.KindTxLog, maxDate.Unix(), pq.Array(tagValues), topic, limit, offset}

	rows, err := n.ndb.Query(query, args...)
	if err != nil {
		return logs, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, pubkey, content, sig string
		var createdAt int64
		var kind int
		var tags json.RawMessage

		err := rows.Scan(&id, &pubkey, &createdAt, &kind, &content, &sig, &tags)
		if err != nil {
			return nil, err
		}

		var nlog nostreth.TxLogEvent
		err = json.Unmarshal([]byte(content), &nlog)
		if err != nil {
			return nil, err
		}

		var log relay.LegacyLog

		// standard properties
		log.Hash = nlog.LogData.Hash
		log.TxHash = nlog.LogData.TxHash
		log.CreatedAt = nlog.LogData.CreatedAt
		log.UpdatedAt = nlog.LogData.UpdatedAt
		log.Nonce = nlog.LogData.Nonce
		log.Sender = nlog.LogData.Sender
		log.To = nlog.LogData.To
		log.Value = nlog.LogData.Value
		log.Data = nlog.LogData.Data

		// hard coded because we stopped doing optimistic indexing
		log.Status = relay.LegacyLogStatusSuccess

		// v1 requires the message as extra data, attempt to find a message
		mentionEvent, err := n.GetMentionEvent(id)
		if err != nil {
			// If no mention event found, continue without extra data
			log.ExtraData = nil
		} else {
			extraData := &relay.ExtraData{
				Description: mentionEvent.Content,
			}

			var extraDataJSON json.RawMessage
			extraDataJSON, err = json.Marshal(extraData)
			if err != nil {
				return nil, err
			}

			log.ExtraData = &extraDataJSON
		}

		logs = append(logs, &log)
	}

	return logs, nil
}

// GetAllNewLogs returns the logs for a given contract and topic from a given date
func (n *Nostr) GetAllNewLogs(contract string, topic string, fromDate time.Time, limit, offset int) ([]*relay.LegacyLog, error) {
	logs := []*relay.LegacyLog{}

	// Collect unique values for tagvalues query
	tagValues := []string{contract}

	// Query the event table for tx_log events with pagination and filtering
	query := `
		SELECT id, pubkey, created_at, kind, content, sig, tags
		FROM event
		WHERE kind = $1 
		AND created_at >= $2
		AND tagvalues @> $3
		AND EXISTS (
			SELECT 1
			FROM jsonb_array_elements(tags) AS tag
			WHERE tag->>0 = 'topic' AND tag->>1 = $4
		)
		ORDER BY created_at DESC
		LIMIT $5 OFFSET $6
	`

	args := []any{nostreth.KindTxLog, fromDate.Unix(), pq.Array(tagValues), topic, limit, offset}

	rows, err := n.ndb.Query(query, args...)
	if err != nil {
		return logs, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, pubkey, content, sig string
		var createdAt int64
		var kind int
		var tags json.RawMessage

		err := rows.Scan(&id, &pubkey, &createdAt, &kind, &content, &sig, &tags)
		if err != nil {
			return nil, err
		}

		var nlog nostreth.TxLogEvent
		err = json.Unmarshal([]byte(content), &nlog)
		if err != nil {
			return nil, err
		}

		var log relay.LegacyLog

		// standard properties
		log.Hash = nlog.LogData.Hash
		log.TxHash = nlog.LogData.TxHash
		log.CreatedAt = nlog.LogData.CreatedAt
		log.UpdatedAt = nlog.LogData.UpdatedAt
		log.Nonce = nlog.LogData.Nonce
		log.Sender = nlog.LogData.Sender
		log.To = nlog.LogData.To
		log.Value = nlog.LogData.Value
		log.Data = nlog.LogData.Data

		// hard coded because we stopped doing optimistic indexing
		log.Status = relay.LegacyLogStatusSuccess

		// v1 requires the message as extra data, attempt to find a message
		mentionEvent, err := n.GetMentionEvent(id)
		if err != nil {
			// If no mention event found, continue without extra data
			log.ExtraData = nil
		} else {
			extraData := &relay.ExtraData{
				Description: mentionEvent.Content,
			}

			var extraDataJSON json.RawMessage
			extraDataJSON, err = json.Marshal(extraData)
			if err != nil {
				return nil, err
			}

			log.ExtraData = &extraDataJSON
		}

		logs = append(logs, &log)
	}

	return logs, nil
}

// GetPaginatedLogs returns the logs for a given contract and topic with data filtering support
func (n *Nostr) GetPaginatedLogs(contract string, topic string, maxDate time.Time, dataFilters, dataFilters2 map[string]any, limit, offset int) ([]*relay.LegacyLog, error) {
	logs := []*relay.LegacyLog{}

	// Collect unique values from both dataFilters and dataFilters2, plus contract
	uniqueValues := make(map[string]bool)

	// Add contract to unique values
	uniqueValues[strings.Trim(contract, " ")] = true

	// Add values from dataFilters
	for _, value := range dataFilters {
		if strValue, ok := value.(string); ok {
			uniqueValues[strings.Trim(strValue, " ")] = true
		}
	}

	// Add values from dataFilters2
	for _, value := range dataFilters2 {
		if strValue, ok := value.(string); ok {
			uniqueValues[strings.Trim(strValue, " ")] = true
		}
	}

	// Convert map keys to slice for SQL array
	var tagValues []string
	for value := range uniqueValues {
		tagValues = append(tagValues, value)
	}

	// Base query for tx_log events with pagination and filtering
	query := `
		SELECT id, pubkey, created_at, kind, content, sig, tags
		FROM event
		WHERE kind = $1 
		AND created_at <= $2
		AND tagvalues @> $3
		AND EXISTS (
			SELECT 1
			FROM jsonb_array_elements(tags) AS tag
			WHERE tag->>0 = 'topic' AND tag->>1 = $4
		)
		ORDER BY created_at DESC
		LIMIT $5 OFFSET $6
	`

	args := []any{nostreth.KindTxLog, maxDate.Unix(), pq.Array(tagValues), topic, limit, offset}

	rows, err := n.ndb.Query(query, args...)
	if err != nil {
		return logs, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, pubkey, content, sig string
		var createdAt int64
		var kind int
		var tags json.RawMessage

		err := rows.Scan(&id, &pubkey, &createdAt, &kind, &content, &sig, &tags)
		if err != nil {
			return nil, err
		}

		var nlog nostreth.TxLogEvent
		err = json.Unmarshal([]byte(content), &nlog)
		if err != nil {
			return nil, err
		}

		var log relay.LegacyLog

		// standard properties
		log.Hash = nlog.LogData.Hash
		log.TxHash = nlog.LogData.TxHash
		log.CreatedAt = nlog.LogData.CreatedAt
		log.UpdatedAt = nlog.LogData.UpdatedAt
		log.Nonce = nlog.LogData.Nonce
		log.Sender = nlog.LogData.Sender
		log.To = nlog.LogData.To
		log.Value = nlog.LogData.Value
		log.Data = nlog.LogData.Data

		// hard coded because we stopped doing optimistic indexing
		log.Status = relay.LegacyLogStatusSuccess

		// v1 requires the message as extra data, attempt to find a message
		mentionEvent, err := n.GetMentionEvent(id)
		if err != nil {
			// If no mention event found, continue without extra data
			log.ExtraData = nil
		} else {
			extraData := &relay.ExtraData{
				Description: mentionEvent.Content,
			}

			var extraDataJSON json.RawMessage
			extraDataJSON, err = json.Marshal(extraData)
			if err != nil {
				return nil, err
			}

			log.ExtraData = &extraDataJSON
		}

		logs = append(logs, &log)
	}

	return logs, nil
}

// GetNewLogs returns the logs for a given contract and topic from a given date with data filtering support
func (n *Nostr) GetNewLogs(contract string, topic string, fromDate time.Time, dataFilters, dataFilters2 map[string]any, limit, offset int) ([]*relay.LegacyLog, error) {
	logs := []*relay.LegacyLog{}

	// Collect unique values from both dataFilters and dataFilters2, plus contract
	uniqueValues := make(map[string]bool)

	// Add contract to unique values
	uniqueValues[strings.Trim(contract, " ")] = true

	// Add values from dataFilters
	for _, value := range dataFilters {
		if strValue, ok := value.(string); ok {
			uniqueValues[strings.Trim(strValue, " ")] = true
		}
	}

	// Add values from dataFilters2
	for _, value := range dataFilters2 {
		if strValue, ok := value.(string); ok {
			uniqueValues[strings.Trim(strValue, " ")] = true
		}
	}

	// Convert map keys to slice for SQL array
	var tagValues []string
	for value := range uniqueValues {
		tagValues = append(tagValues, value)
	}

	// Base query for tx_log events with pagination and filtering
	query := `
		SELECT id, pubkey, created_at, kind, content, sig, tags
		FROM event
		WHERE kind = $1 
		AND created_at >= $2
		AND tagvalues @> $3
		AND EXISTS (
			SELECT 1
			FROM jsonb_array_elements(tags) AS tag
			WHERE tag->>0 = 'topic' AND tag->>1 = $4
		)
	`

	args := []any{nostreth.KindTxLog, fromDate.Unix(), pq.Array(tagValues), topic}

	// Add pagination
	query += ` ORDER BY created_at DESC LIMIT $5 OFFSET $6`
	args = append(args, limit, offset)

	rows, err := n.ndb.Query(query, args...)
	if err != nil {
		println("error getting new logs", err.Error())
		return logs, err
	}
	defer rows.Close()

	for rows.Next() {
		var id, pubkey, content, sig string
		var createdAt int64
		var kind int
		var tags json.RawMessage

		err := rows.Scan(&id, &pubkey, &createdAt, &kind, &content, &sig, &tags)
		if err != nil {
			return nil, err
		}

		var nlog nostreth.TxLogEvent
		err = json.Unmarshal([]byte(content), &nlog)
		if err != nil {
			return nil, err
		}

		var log relay.LegacyLog

		// standard properties
		log.Hash = nlog.LogData.Hash
		log.TxHash = nlog.LogData.TxHash
		log.CreatedAt = nlog.LogData.CreatedAt
		log.UpdatedAt = nlog.LogData.UpdatedAt
		log.Nonce = nlog.LogData.Nonce
		log.Sender = nlog.LogData.Sender
		log.To = nlog.LogData.To
		log.Value = nlog.LogData.Value
		log.Data = nlog.LogData.Data

		// hard coded because we stopped doing optimistic indexing
		log.Status = relay.LegacyLogStatusSuccess

		// v1 requires the message as extra data, attempt to find a message
		mentionEvent, err := n.GetMentionEvent(id)
		if err != nil {
			// If no mention event found, continue without extra data
			log.ExtraData = nil
		} else {
			extraData := &relay.ExtraData{
				Description: mentionEvent.Content,
			}

			var extraDataJSON json.RawMessage
			extraDataJSON, err = json.Marshal(extraData)
			if err != nil {
				return nil, err
			}

			log.ExtraData = &extraDataJSON
		}

		logs = append(logs, &log)
	}

	return logs, nil
}
