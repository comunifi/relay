package db

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/citizenapp2/relay/pkg/common"
	"github.com/citizenapp2/relay/pkg/relay"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type LogDB struct {
	ctx    context.Context
	suffix string
	db     *pgxpool.Pool
	rdb    *pgxpool.Pool
	datadb *DataDB
}

// NewLogDB creates a new DB
func NewLogDB(ctx context.Context, db, rdb *pgxpool.Pool, name string, datadb *DataDB) (*LogDB, error) {
	txdb := &LogDB{
		ctx:    ctx,
		suffix: name,
		db:     db,
		rdb:    rdb,
		datadb: datadb,
	}

	return txdb, nil
}

// createLogTable creates a table dest store logs in the given db
func (db *LogDB) CreateLogTable() error {
	_, err := db.db.Exec(db.ctx, fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS t_logs_%s(
		hash TEXT NOT NULL PRIMARY KEY,
		tx_hash text NOT NULL,
		created_at timestamp NOT NULL DEFAULT current_timestamp,
		updated_at timestamp NOT NULL DEFAULT current_timestamp,
		nonce integer NOT NULL,
		sender text NOT NULL,
		dest text NOT NULL,
		value text NOT NULL,
		data jsonb DEFAULT NULL,
		status text NOT NULL DEFAULT 'success'
	);
	`, db.suffix))

	return err
}

// createLogTableIndexes creates the indexes for logs in the given db
func (db *LogDB) CreateLogTableIndexes() error {
	suffix := common.ShortenName(db.suffix, 6)

	_, err := db.db.Exec(db.ctx, fmt.Sprintf(`
	CREATE INDEX IF NOT EXISTS idx_logs_%s_tx_hash ON t_logs_%s (tx_hash);
	`, suffix, db.suffix))
	if err != nil {
		return err
	}

	// filtering on contract address
	_, err = db.db.Exec(db.ctx, fmt.Sprintf(`
	CREATE INDEX IF NOT EXISTS idx_logs_%s_dest ON t_logs_%s (dest);
	`, suffix, db.suffix))
	if err != nil {
		return err
	}

	// filtering on event topic for a given contract
	_, err = db.db.Exec(db.ctx, fmt.Sprintf(`
	CREATE INDEX IF NOT EXISTS idx_logs_%s_dest_date ON t_logs_%s (dest, created_at);
	`, suffix, db.suffix))
	if err != nil {
		return err
	}

	// filtering on event topic for a given contract for a range of dates
	_, err = db.db.Exec(db.ctx, fmt.Sprintf(`
	CREATE INDEX IF NOT EXISTS idx_logs_%s_dest_topic_date ON t_logs_%s (dest, (data->>'topic'), created_at);
	`, suffix, db.suffix))
	if err != nil {
		return err
	}

	// filtering by address [CANNOT DO THIS ANYMORE]
	// _, err = db.db.Exec(db.ctx, fmt.Sprintf(`
	// CREATE INDEX IF NOT EXISTS idx_logs_%s_to_addr ON t_logs_%s (to_addr);
	// `, suffix, db.suffix))
	// if err != nil {
	// 	return err
	// }

	// _, err = db.db.Exec(db.ctx, fmt.Sprintf(`
	// CREATE INDEX IF NOT EXISTS idx_logs_%s_from_addr ON t_logs_%s (from_addr);
	// `, suffix, db.suffix))
	// if err != nil {
	// 	return err
	// }

	// // single-token queries
	// _, err = db.db.Exec(db.ctx, fmt.Sprintf(`
	// CREATE INDEX IF NOT EXISTS idx_logs_%s_date_from_token_id_from_addr_simple ON t_logs_%s (created_at, token_id, from_addr);
	// `, suffix, db.suffix))
	// if err != nil {
	// 	return err
	// }

	// _, err = db.db.Exec(db.ctx, fmt.Sprintf(`
	// CREATE INDEX IF NOT EXISTS idx_logs_%s_date_from_token_id_to_addr_simple ON t_logs_%s (created_at, token_id, to_addr);
	// `, suffix, db.suffix))
	// if err != nil {
	// 	return err
	// }

	// // sending queries
	// _, err = db.db.Exec(db.ctx, fmt.Sprintf(`
	// CREATE INDEX IF NOT EXISTS idx_logs_%s_status_date_from_tx_hash ON t_logs_%s (status, created_at, tx_hash);
	// `, suffix, db.suffix))
	// if err != nil {
	// 	return err
	// }

	// // finding optimistic transactions
	// _, err = db.db.Exec(db.ctx, fmt.Sprintf(`
	// 	CREATE INDEX IF NOT EXISTS idx_logs_%s_to_addr_from_addr_value ON t_logs_%s (to_addr, from_addr, value);
	// 	`, suffix, db.suffix))
	// if err != nil {
	// 	return err
	// }

	return nil
}

// AddLog adds a log dest the db
func (db *LogDB) AddLog(lg *relay.Log) error {

	// start transaction
	tx, err := db.db.BeginTx(db.ctx, pgx.TxOptions{
		IsoLevel:       pgx.ReadCommitted,
		AccessMode:     pgx.ReadWrite,
		DeferrableMode: pgx.NotDeferrable,
	})
	if err != nil {
		return err
	}

	// Use a flag to track if we've committed the transaction
	committed := false
	defer func() {
		if !committed {
			tx.Rollback(db.ctx)
		}
	}()

	// insert log on conflict do nothing
	_, err = tx.Exec(db.ctx, fmt.Sprintf(`
	INSERT INTO t_logs_%s (hash, tx_hash, nonce, sender, dest, value, data, status, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	ON CONFLICT (hash) DO NOTHING
	`, db.suffix), lg.Hash, lg.TxHash, lg.Nonce, lg.Sender, lg.To, lg.Value.String(), lg.Data, lg.Status, lg.CreatedAt, lg.UpdatedAt)

	if err != nil {
		return err
	}

	// If ExtraData exists, store it in the data table
	if lg.ExtraData != nil {
		err = db.datadb.UpsertData(tx, lg.Hash, lg.ExtraData)
		if err != nil {
			return err
		}
	}

	// Commit the transaction
	err = tx.Commit(db.ctx)
	if err != nil {
		return err
	}

	// Mark as committed so the deferred rollback won't execute
	committed = true
	return nil
}

// AddLogs adds a list of logs dest the db
func (db *LogDB) AddLogs(lg []*relay.Log) error {
	// start transaction
	tx, err := db.db.BeginTx(db.ctx, pgx.TxOptions{
		IsoLevel:       pgx.ReadCommitted,
		AccessMode:     pgx.ReadWrite,
		DeferrableMode: pgx.NotDeferrable,
	})
	if err != nil {
		return err
	}

	// Use a flag to track if we've committed the transaction
	committed := false
	defer func() {
		if !committed {
			tx.Rollback(db.ctx)
		}
	}()

	for _, t := range lg {
		_, err := tx.Exec(db.ctx, fmt.Sprintf(`
			INSERT INTO t_logs_%s (hash, tx_hash, nonce, sender, dest, value, data, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (hash) DO UPDATE SET
				tx_hash = EXCLUDED.tx_hash,
				nonce = EXCLUDED.nonce,
				sender = CASE
					WHEN EXCLUDED.sender = '' THEN t_logs_%s.sender
					ELSE COALESCE(EXCLUDED.sender, t_logs_%s.sender)
				END,
				dest = EXCLUDED.dest,
				value = EXCLUDED.value,
				data = COALESCE(EXCLUDED.data, t_logs_%s.data),
				status = EXCLUDED.status,
				created_at = EXCLUDED.created_at,
				updated_at = EXCLUDED.updated_at
			`, db.suffix, db.suffix, db.suffix, db.suffix), t.Hash, t.TxHash, t.Nonce, t.Sender, t.To, t.Value.String(), t.Data, t.Status, t.CreatedAt, t.UpdatedAt)
		if err != nil {
			return err
		}

		// If ExtraData exists, store it in the data table
		if t.ExtraData != nil {
			err = db.datadb.UpsertData(tx, t.Hash, t.ExtraData)
			if err != nil {
				return err
			}
		}
	}

	// Commit the transaction
	err = tx.Commit(db.ctx)
	if err != nil {
		return err
	}

	// Mark as committed so the deferred rollback won't execute
	committed = true
	return nil
}

// SetStatus sets the status of a log dest pending
func (db *LogDB) SetStatus(status, hash string) error {
	// if status is success, don't update
	_, err := db.db.Exec(db.ctx, fmt.Sprintf(`
	UPDATE t_logs_%s SET status = $1 WHERE hash = $2 AND status != 'success'
	`, db.suffix), status, hash)

	return err
}

// RemoveLog removes a sending log from the db
func (db *LogDB) RemoveLog(hash string) error {
	_, err := db.db.Exec(db.ctx, fmt.Sprintf(`
	DELETE FROM t_logs_%s WHERE hash = $1 AND status != 'success'
	`, db.suffix), hash)

	return err
}

// RemoveOldInProgressLogs removes any log that is not success or fail from the db
func (db *LogDB) RemoveOldInProgressLogs() error {
	old := time.Now().UTC().Add(-30 * time.Second)

	_, err := db.db.Exec(db.ctx, fmt.Sprintf(`
	DELETE FROM t_logs_%s WHERE created_at <= $1 AND status IN ('sending', 'pending')
	`, db.suffix), old)

	return err
}

// GetLog returns the log for a given hash
func (db *LogDB) GetLog(hash string) (*relay.Log, error) {
	var log relay.Log
	var value string
	var extraData *json.RawMessage

	row := db.rdb.QueryRow(db.ctx, fmt.Sprintf(`
		SELECT l.hash, l.tx_hash, l.created_at, l.updated_at, l.nonce, l.sender, l.dest, l.value, l.data, l.status, d.data as extra_data
		FROM t_logs_%s l
		LEFT JOIN t_logs_data_%s d ON l.hash = d.hash
		WHERE l.hash = $1
		`, db.suffix, db.suffix), hash)

	err := row.Scan(&log.Hash, &log.TxHash, &log.CreatedAt, &log.UpdatedAt, &log.Nonce, &log.Sender, &log.To, &value, &log.Data, &log.Status, &extraData)
	if err != nil {
		return nil, err
	}

	log.Value = new(big.Int)
	log.Value.SetString(value, 10)
	log.ExtraData = extraData

	return &log, nil
}

// GetAllPaginatedLogs returns the logs paginated
func (db *LogDB) GetAllPaginatedLogs(contract string, topic string, maxDate time.Time, limit, offset int) ([]*relay.Log, error) {
	logs := []*relay.Log{}

	query := fmt.Sprintf(`
	SELECT l.hash, l.tx_hash, l.created_at, l.updated_at, l.nonce, l.sender, l.dest, l.value, l.data, l.status, d.data as extra_data
	FROM t_logs_%s l
	LEFT JOIN t_logs_data_%s d ON l.hash = d.hash
	WHERE l.dest = $1 AND l.data->>'topic' = $2 AND l.created_at <= $3
	ORDER BY l.created_at DESC
	LIMIT $4 OFFSET $5
	`, db.suffix, db.suffix)

	args := []any{contract, topic, maxDate, limit, offset}

	rows, err := db.rdb.Query(db.ctx, query, args...)
	if err != nil {
		if err == pgx.ErrNoRows {
			return logs, nil
		}

		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var log relay.Log
		var value string
		var extraData *json.RawMessage

		err := rows.Scan(&log.Hash, &log.TxHash, &log.CreatedAt, &log.UpdatedAt, &log.Nonce, &log.Sender, &log.To, &value, &log.Data, &log.Status, &extraData)
		if err != nil {
			return nil, err
		}

		log.Value = new(big.Int)
		log.Value.SetString(value, 10)
		log.ExtraData = extraData

		logs = append(logs, &log)
	}

	return logs, nil
}

// GetPaginatedLogs returns the logs for a given from_addr or to_addr paginated
func (db *LogDB) GetPaginatedLogs(contract string, topic string, maxDate time.Time, dataFilters, dataFilters2 map[string]any, limit, offset int) ([]*relay.Log, error) {
	logs := []*relay.Log{}

	query := fmt.Sprintf(`
		SELECT l.hash, l.tx_hash, l.created_at, l.updated_at, l.nonce, l.sender, l.dest, l.value, l.data, l.status, d.data as extra_data
		FROM t_logs_%s l
		LEFT JOIN t_logs_data_%s d ON l.hash = d.hash
		WHERE l.dest = $1 AND l.data->>'topic' = $2 AND l.created_at <= $3
		`, db.suffix, db.suffix)

	args := []any{contract, topic, maxDate}

	orderLimit := `
		ORDER BY l.created_at DESC
		LIMIT $4 OFFSET $5
		`

	if len(dataFilters) > 0 {
		topicQuery, topicArgs := relay.GenerateJSONBQuery("l.", len(args)+1, dataFilters)

		query += `AND `
		query += topicQuery

		args = append(args, topicArgs...)

		if len(dataFilters2) > 0 {
			// I'm being lazy here, could be dynamic
			query += fmt.Sprintf(`
				UNION ALL
				SELECT l.hash, l.tx_hash, l.created_at, l.updated_at, l.nonce, l.sender, l.dest, l.value, l.data, l.status, d.data as extra_data
				FROM t_logs_%s l
				LEFT JOIN t_logs_data_%s d ON l.hash = d.hash
				WHERE l.dest = $%d AND l.data->>'topic' = $%d AND l.created_at <= $%d
				`, db.suffix, db.suffix, len(args)+1, len(args)+2, len(args)+3)

			args = append(args, contract, topic, maxDate)

			topicQuery2, topicArgs2 := relay.GenerateJSONBQuery("l.", len(args)+1, dataFilters2)

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

	rows, err := db.rdb.Query(db.ctx, query, args...)
	if err != nil {
		if err == pgx.ErrNoRows {
			return logs, nil
		}

		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var log relay.Log
		var value string
		var extraData *json.RawMessage

		err := rows.Scan(&log.Hash, &log.TxHash, &log.CreatedAt, &log.UpdatedAt, &log.Nonce, &log.Sender, &log.To, &value, &log.Data, &log.Status, &extraData)
		if err != nil {
			return nil, err
		}

		log.Value = new(big.Int)
		log.Value.SetString(value, 10)
		log.ExtraData = extraData

		logs = append(logs, &log)
	}

	return logs, nil
}

// GetAllNewLogs returns the logs for a given from_addr or to_addr from a given date
func (db *LogDB) GetAllNewLogs(contract string, topic string, fromDate time.Time, limit, offset int) ([]*relay.Log, error) {
	logs := []*relay.Log{}

	query := fmt.Sprintf(`
		SELECT l.hash, l.tx_hash, l.created_at, l.nonce, l.sender, l.dest, l.value, l.data, l.status, d.data as extra_data
		FROM t_logs_%s l
		LEFT JOIN t_logs_data_%s d ON l.hash = d.hash
		WHERE l.dest = $1 AND l.data->>'topic' = $2 AND l.created_at >= $3
		`, db.suffix, db.suffix)

	args := []any{contract, topic, fromDate}

	orderLimit := `
		ORDER BY l.created_at DESC
		LIMIT $4 OFFSET $5
		`

	args = append(args, limit, offset)

	query += orderLimit

	rows, err := db.rdb.Query(db.ctx, query, args...)
	if err != nil {
		if err == pgx.ErrNoRows {
			return logs, nil
		}

		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var log relay.Log
		var value string
		var extraData *json.RawMessage

		err := rows.Scan(&log.Hash, &log.TxHash, &log.CreatedAt, &log.Nonce, &log.Sender, &log.To, &value, &log.Data, &log.Status, &extraData)
		if err != nil {
			return nil, err
		}

		log.Value = new(big.Int)
		log.Value.SetString(value, 10)
		log.ExtraData = extraData

		logs = append(logs, &log)
	}

	return logs, nil
}

// GetNewLogs returns the logs for a given from_addr or to_addr from a given date
func (db *LogDB) GetNewLogs(contract string, topic string, fromDate time.Time, dataFilters, dataFilters2 map[string]any, limit, offset int) ([]*relay.Log, error) {
	logs := []*relay.Log{}

	query := fmt.Sprintf(`
		SELECT l.hash, l.tx_hash, l.created_at, l.nonce, l.sender, l.dest, l.value, l.data, l.status, d.data as extra_data
		FROM t_logs_%s l
		LEFT JOIN t_logs_data_%s d ON l.hash = d.hash
		WHERE l.dest = $1 AND l.data->>'topic' = $2 AND l.created_at >= $3
		`, db.suffix, db.suffix)

	args := []any{contract, topic, fromDate}

	orderLimit := `
		ORDER BY l.created_at DESC
		LIMIT $3 OFFSET $4
		`
	if len(dataFilters) > 0 {
		topicQuery, topicArgs := relay.GenerateJSONBQuery("l.", len(args)+1, dataFilters)

		query += `AND `
		query += topicQuery

		args = append(args, topicArgs...)

		if len(dataFilters2) > 0 {
			// I'm being lazy here, could be dynamic
			query += fmt.Sprintf(`
				UNION ALL
				SELECT l.hash, l.tx_hash, l.created_at, l.nonce, l.sender, l.dest, l.value, l.data, l.status, d.data as extra_data
				FROM t_logs_%s l
				LEFT JOIN t_logs_data_%s d ON l.hash = d.hash
				WHERE l.dest = $%d AND l.data->>'topic' = $%d AND l.created_at >= $%d
				`, db.suffix, db.suffix, len(args)+1, len(args)+2, len(args)+3)

			args = append(args, contract, topic, fromDate)

			topicQuery2, topicArgs2 := relay.GenerateJSONBQuery("l.", len(args)+1, dataFilters2)

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

	rows, err := db.rdb.Query(db.ctx, query, args...)
	if err != nil {
		if err == pgx.ErrNoRows {
			return logs, nil
		}

		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var log relay.Log
		var value string
		var extraData *json.RawMessage

		err := rows.Scan(&log.Hash, &log.TxHash, &log.CreatedAt, &log.Nonce, &log.Sender, &log.To, &value, &log.Data, &log.Status, &extraData)
		if err != nil {
			return nil, err
		}

		log.Value = new(big.Int)
		log.Value.SetString(value, 10)
		log.ExtraData = extraData

		logs = append(logs, &log)
	}

	return logs, nil
}

// UpdateLogsWithDB returns the logs with data updated from the db
func (db *LogDB) UpdateLogsWithDB(txs []*relay.Log) ([]*relay.Log, error) {
	if len(txs) == 0 {
		return txs, nil
	}

	// Convert the log hashes dest a comma-separated string
	hashStr := ""
	for _, lg := range txs {
		// if last item, don't add a trailing comma
		if lg == txs[len(txs)-1] {
			hashStr += fmt.Sprintf("('%s')", lg.Hash)
			continue
		}

		hashStr += fmt.Sprintf("('%s'),", lg.Hash)
	}

	rows, err := db.rdb.Query(db.ctx, fmt.Sprintf(`
		WITH b(hash) AS (
			VALUES
			%s
		)
		SELECT lg.hash, lg.tx_hash, lg.created_at, lg.nonce, lg.sender, lg.dest, lg.value, lg.data, lg.status, d.data as extra_data
		FROM t_logs_%s lg
		JOIN b ON lg.hash = b.hash
		LEFT JOIN t_logs_data_%s d ON lg.hash = d.hash;
		`, hashStr, db.suffix, db.suffix))
	if err != nil {
		if err == pgx.ErrNoRows {
			return txs, nil
		}

		return nil, err
	}
	defer rows.Close()

	mtxs := map[string]*relay.Log{}
	for _, lg := range txs {
		mtxs[lg.Hash] = lg
	}

	for rows.Next() {
		var log relay.Log
		var value string
		var extraData *json.RawMessage

		err := rows.Scan(&log.Hash, &log.TxHash, &log.CreatedAt, &log.Nonce, &log.Sender, &log.To, &value, &log.Data, &log.Status, &extraData)
		if err != nil {
			return nil, err
		}

		log.Value = new(big.Int)
		log.Value.SetString(value, 10)
		log.ExtraData = extraData

		// check if exists
		if _, ok := mtxs[log.Hash]; !ok {
			continue
		}

		// update the log
		mtxs[log.Hash].Update(&log)
	}

	return txs, nil
}
