package nostr

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	nostreth "github.com/comunifi/nostr-eth"
	"github.com/comunifi/relay/pkg/relay"
)

// GetLog returns the log for a given hash by querying the "d" tag
func (n *Nostr) GetLog(hash, chainID string) (*relay.LegacyLog, error) {
	var log relay.LegacyLog

	// Query the event table for events where the "t" tag matches the chain ID and "d" tag matches the hash
	row := n.ndb.QueryRow(`
		SELECT id, pubkey, created_at, kind, content, sig, tags
		FROM event
		WHERE kind = $1 
		AND EXISTS (
			SELECT 1
			FROM jsonb_array_elements(tags) AS tag
			WHERE tag->>0 = 't' AND tag->>1 = $2
		)
		AND EXISTS (
			SELECT 1
			FROM jsonb_array_elements(tags) AS tag
			WHERE tag->>0 = 'd' AND tag->>1 = $3
		)
		LIMIT 1
	`, nostreth.KindTxLog, chainID, hash)

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
		if err == sql.ErrNoRows {
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

	// Query the event table for tx_log events with pagination and filtering
	query := `
		SELECT id, pubkey, created_at, kind, content, sig, tags
		FROM event
		WHERE kind = $1 
		AND created_at <= $2
		AND EXISTS (
			SELECT 1
			FROM jsonb_array_elements(tags) AS tag
			WHERE tag->>0 = 'p' AND tag->>1 = $3
		)
		AND EXISTS (
			SELECT 1
			FROM jsonb_array_elements(tags) AS tag
			WHERE tag->>0 = 'topic' AND tag->>1 = $4
		)
		ORDER BY created_at DESC
		LIMIT $5 OFFSET $6
	`

	args := []any{nostreth.KindTxLog, maxDate.Unix(), contract, topic, limit, offset}

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

	// Query the event table for tx_log events with pagination and filtering
	query := `
		SELECT id, pubkey, created_at, kind, content, sig, tags
		FROM event
		WHERE kind = $1 
		AND created_at >= $2
		AND EXISTS (
			SELECT 1
			FROM jsonb_array_elements(tags) AS tag
			WHERE tag->>0 = 'p' AND tag->>1 = $3
		)
		AND EXISTS (
			SELECT 1
			FROM jsonb_array_elements(tags) AS tag
			WHERE tag->>0 = 'topic' AND tag->>1 = $4
		)
		ORDER BY created_at DESC
		LIMIT $5 OFFSET $6
	`

	args := []any{nostreth.KindTxLog, fromDate.Unix(), contract, topic, limit, offset}

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

	// Base query for tx_log events with pagination and filtering
	query := `
		SELECT id, pubkey, created_at, kind, content, sig, tags
		FROM event
		WHERE kind = $1 
		AND created_at <= $2
		AND EXISTS (
			SELECT 1
			FROM jsonb_array_elements(tags) AS tag
			WHERE tag->>0 = 'p' AND tag->>1 = $3
		)
		AND EXISTS (
			SELECT 1
			FROM jsonb_array_elements(tags) AS tag
			WHERE tag->>0 = 'topic' AND tag->>1 = $4
		)
	`

	args := []any{nostreth.KindTxLog, maxDate.Unix(), contract, topic}

	orderLimit := `
		ORDER BY created_at DESC
		LIMIT $5 OFFSET $6
	`

	if len(dataFilters) > 0 {
		// Parse the content as JSON to access the data field for filtering
		topicQuery, topicArgs := relay.GenerateJSONBQuery("content::jsonb->'log_data'->", len(args)+1, dataFilters)

		query += `AND `
		query += topicQuery

		args = append(args, topicArgs...)

		if len(dataFilters2) > 0 {
			// Add UNION ALL for second set of filters
			query += fmt.Sprintf(`
				UNION ALL
				SELECT id, pubkey, created_at, kind, content, sig, tags
				FROM event
				WHERE kind = $%d 
				AND created_at <= $%d
				AND EXISTS (
					SELECT 1
					FROM jsonb_array_elements(tags) AS tag
					WHERE tag->>0 = 'p' AND tag->>1 = $%d
				)
				AND EXISTS (
					SELECT 1
					FROM jsonb_array_elements(tags) AS tag
					WHERE tag->>0 = 'topic' AND tag->>1 = $%d
				)
				`, len(args)+1, len(args)+2, len(args)+3, len(args)+4)

			args = append(args, nostreth.KindTxLog, maxDate.Unix(), contract, topic)

			topicQuery2, topicArgs2 := relay.GenerateJSONBQuery("content::jsonb->'log_data'->", len(args)+1, dataFilters2)

			query += `AND `
			query += topicQuery2

			args = append(args, topicArgs2...)
		}

		argsLength := len(args)

		orderLimit = fmt.Sprintf(`
			ORDER BY created_at DESC LIMIT $%d OFFSET $%d
			`, argsLength+1, argsLength+2)
	}

	args = append(args, limit, offset)

	query += orderLimit

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

	// Base query for tx_log events with pagination and filtering
	query := `
		SELECT id, pubkey, created_at, kind, content, sig, tags
		FROM event
		WHERE kind = $1 
		AND created_at >= $2
		AND EXISTS (
			SELECT 1
			FROM jsonb_array_elements(tags) AS tag
			WHERE tag->>0 = 'p' AND tag->>1 = $3
		)
		AND EXISTS (
			SELECT 1
			FROM jsonb_array_elements(tags) AS tag
			WHERE tag->>0 = 'topic' AND tag->>1 = $4
		)
	`

	args := []any{nostreth.KindTxLog, fromDate.Unix(), contract, topic}

	orderLimit := `
		ORDER BY created_at DESC
		LIMIT $5 OFFSET $6
	`

	if len(dataFilters) > 0 {
		// Parse the content as JSON to access the data field for filtering
		topicQuery, topicArgs := relay.GenerateJSONBQuery("content::jsonb->'log_data'->", len(args)+1, dataFilters)

		query += `AND `
		query += topicQuery

		args = append(args, topicArgs...)

		if len(dataFilters2) > 0 {
			// Add UNION ALL for second set of filters
			query += fmt.Sprintf(`
				UNION ALL
				SELECT id, pubkey, created_at, kind, content, sig, tags
				FROM event
				WHERE kind = $%d 
				AND created_at >= $%d
				AND EXISTS (
					SELECT 1
					FROM jsonb_array_elements(tags) AS tag
					WHERE tag->>0 = 'p' AND tag->>1 = $%d
				)
				AND EXISTS (
					SELECT 1
					FROM jsonb_array_elements(tags) AS tag
					WHERE tag->>0 = 'topic' AND tag->>1 = $%d
				)
				`, len(args)+1, len(args)+2, len(args)+3, len(args)+4)

			args = append(args, nostreth.KindTxLog, fromDate.Unix(), contract, topic)

			topicQuery2, topicArgs2 := relay.GenerateJSONBQuery("content::jsonb->'log_data'->", len(args)+1, dataFilters2)

			query += `AND `
			query += topicQuery2

			args = append(args, topicArgs2...)
		}

		argsLength := len(args)

		orderLimit = fmt.Sprintf(`
			ORDER BY created_at DESC LIMIT $%d OFFSET $%d
			`, argsLength+1, argsLength+2)
	}

	args = append(args, limit, offset)

	query += orderLimit

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
