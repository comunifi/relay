package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/citizenapp2/relay/cmd/migrate/mtransfer"
	"github.com/citizenapp2/relay/internal/config"
	"github.com/citizenapp2/relay/internal/db"
	"github.com/citizenapp2/relay/internal/ethrequest"
	"github.com/citizenapp2/relay/pkg/relay"
	_ "github.com/mattn/go-sqlite3"
)

const (
	transferTopic   = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	batchSize       = 1000
	migrationSuffix = "migration"
)

func main() {
	// Parse command-line flags
	env := flag.String("env", ".env", "path to .env file")
	contractAddress := flag.String("contract", "", "contract address")
	flag.Parse()

	if *contractAddress == "" {
		log.Fatal("contract address is required")
	}

	// Load configuration from .env file
	ctx := context.Background()
	conf, err := config.New(ctx, *env)
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}

	// Open SQLite database
	sqliteDB, err := sql.Open("sqlite3", "./cw.db")
	if err != nil {
		log.Fatalf("Error opening SQLite database: %v", err)
	}
	defer sqliteDB.Close()

	// Construct PostgreSQL connection string
	evm, err := ethrequest.NewEthService(ctx, conf.RPCURL)
	if err != nil {
		log.Fatal(err)
	}

	chid, err := evm.ChainID()
	if err != nil {
		log.Fatal(err)
	}

	d, err := db.NewDB(chid, conf.DBSecret, conf.DBUser, conf.DBPassword, conf.DBName, conf.DBPort,
		"0.0.0.0", "0.0.0.0")
	if err != nil {
		log.Fatal(err)
	}
	defer d.Close()

	// Perform migration
	err = migrateData(sqliteDB, d.LogDB, chid, *contractAddress)
	if err != nil {
		log.Fatalf("Error during migration: %v", err)
	}

	log.Println("Migration completed successfully")
}

func migrateData(sqliteDB *sql.DB, logDB *db.LogDB, chid *big.Int, contractAddress string) error {
	offset := 0
	for {
		transfers, err := getTransfers(sqliteDB, offset, batchSize, fmt.Sprintf("%s_%s", chid.String(), contractAddress))
		if err != nil {
			return fmt.Errorf("error getting transfers: %v", err)
		}

		if len(transfers) == 0 {
			break
		}

		logs := convertTransfersToLogs(transfers, contractAddress)

		err = logDB.AddLogs(logs)
		if err != nil {
			return fmt.Errorf("error adding logs: %v", err)
		}

		offset += len(transfers)
		log.Printf("Migrated %d transfers", offset)
	}

	return nil
}

func getTransfers(db *sql.DB, offset, limit int, suffix string) ([]*mtransfer.Transfer, error) {
	query := fmt.Sprintf(`
		SELECT hash, tx_hash, token_id, created_at, from_addr, to_addr, nonce, value, data, status
		FROM t_transfers_%s
		ORDER BY created_at
		LIMIT ? OFFSET ?
	`, suffix)

	rows, err := db.Query(query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transfers []*mtransfer.Transfer
	for rows.Next() {
		var t mtransfer.Transfer
		var valueStr string
		err := rows.Scan(&t.Hash, &t.TxHash, &t.TokenID, &t.CreatedAt, &t.From, &t.To, &t.Nonce, &valueStr, &t.Data, &t.Status)
		if err != nil {
			return nil, err
		}
		t.Value = new(big.Int)
		t.Value.SetString(valueStr, 10)
		transfers = append(transfers, &t)
	}

	return transfers, nil
}

func convertTransfersToLogs(transfers []*mtransfer.Transfer, contractAddress string) []*relay.Log {
	var logs []*relay.Log
	for _, t := range transfers {
		data := map[string]interface{}{
			"topic": transferTopic,
			"from":  t.From,
			"to":    t.To,
			"value": t.Value.String(),
		}
		dataJSON, _ := json.Marshal(data)
		dataRaw := json.RawMessage(dataJSON)

		b, err := json.Marshal(t.Data)
		if err != nil {
			log.Fatalf("Error marshalling data: %v", err)
		}

		var extraDataRaw json.RawMessage
		if t.Data != nil {
			extraDataRaw = json.RawMessage(b)
		}

		log := &relay.Log{
			Hash:      t.Hash,
			TxHash:    t.TxHash,
			CreatedAt: t.CreatedAt,
			UpdatedAt: time.Now(),
			Nonce:     0,
			Sender:    t.From,
			To:        contractAddress,
			Value:     big.NewInt(0),
			Data:      &dataRaw,
			ExtraData: &extraDataRaw,
			Status:    relay.LogStatusSuccess,
		}
		logs = append(logs, log)
	}
	return logs
}
