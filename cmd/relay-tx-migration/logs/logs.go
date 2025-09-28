package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	nostreth "github.com/comunifi/nostr-eth"
	"github.com/comunifi/relay/cmd/relay-tx-migration/logs/logdb"
	"github.com/comunifi/relay/internal/ethrequest"
	"github.com/comunifi/relay/pkg/relay"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	nost "github.com/comunifi/relay/internal/nostr"
	"github.com/nbd-wtf/go-nostr"
)

// getERC20Symbol calls the symbol() method on an ERC20 contract
func getERC20Symbol(evm *ethrequest.EthService, contractAddress common.Address) (string, error) {
	// ERC20 symbol() function selector: 0x95d89b41
	symbolSelector := common.Hex2Bytes("95d89b41")

	result, err := evm.CallContract(ethereum.CallMsg{
		To:   &contractAddress,
		Data: symbolSelector,
	}, nil)
	if err != nil {
		return "", fmt.Errorf("failed to call symbol(): %w", err)
	}

	// Decode the result using ABI
	if len(result) == 0 {
		return "", fmt.Errorf("empty result from symbol() call")
	}

	// Create a simple ABI for the symbol() function
	erc20ABI, err := abi.JSON(strings.NewReader(`[{"constant":true,"inputs":[],"name":"symbol","outputs":[{"name":"","type":"string"}],"type":"function"}]`))
	if err != nil {
		return "", fmt.Errorf("failed to parse ABI: %w", err)
	}

	// Decode the result
	var symbol string
	err = erc20ABI.UnpackIntoInterface(&symbol, "symbol", result)
	if err != nil {
		return "", fmt.Errorf("failed to unpack symbol result: %w", err)
	}

	return symbol, nil
}

// getERC20Decimals calls the decimals() method on an ERC20 contract
func getERC20Decimals(evm *ethrequest.EthService, contractAddress common.Address) (uint8, error) {
	// ERC20 decimals() function selector: 0x313ce567
	decimalsSelector := common.Hex2Bytes("313ce567")

	result, err := evm.CallContract(ethereum.CallMsg{
		To:   &contractAddress,
		Data: decimalsSelector,
	}, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to call decimals(): %w", err)
	}

	// Decode the result using ABI
	if len(result) == 0 {
		return 0, fmt.Errorf("empty result from decimals() call")
	}

	// Create a simple ABI for the decimals() function
	erc20ABI, err := abi.JSON(strings.NewReader(`[{"constant":true,"inputs":[],"name":"decimals","outputs":[{"name":"","type":"uint8"}],"type":"function"}]`))
	if err != nil {
		return 0, fmt.Errorf("failed to parse ABI: %w", err)
	}

	// Decode the result
	var decimals uint8
	err = erc20ABI.UnpackIntoInterface(&decimals, "decimals", result)
	if err != nil {
		return 0, fmt.Errorf("failed to unpack decimals result: %w", err)
	}

	return decimals, nil
}

func MigrateLogs(ctx context.Context, evm *ethrequest.EthService, chainID *big.Int, group *string, secretKey, pubkey string, db *logdb.DB, n *nost.Nostr) error {
	events, err := db.EventDB.GetEvents()
	if err != nil {
		return err
	}

	maxDate := time.Now()
	maxDate.AddDate(0, 0, 1)

	for _, event := range events {
		log.Printf("Migrating logs for event: %s", event.Name)
		topic := event.Topic

		offset := 0
		for {
			logs, err := db.LogDB.GetAllPaginatedLogs(event.Contract, topic, maxDate, 100, offset)
			if err != nil {
				return err
			}

			if len(logs) == 0 {
				break
			}

			for _, log := range logs {

				nostrethLog := &nostreth.Log{
					Hash:      log.Hash,
					TxHash:    log.TxHash,
					ChainID:   chainID.String(),
					Topic:     topic,
					CreatedAt: log.CreatedAt,
					UpdatedAt: log.UpdatedAt,
					Nonce:     log.Nonce,
					Sender:    log.Sender,
					To:        log.To,
					Value:     log.Value,
					Data:      log.Data,
				}

				nostrethLog.Hash = nostrethLog.GenerateUniqueHash()

				ev := convertLogToEvent(nostrethLog)

				sev, err := n.SignAndSaveEvent(ctx, ev)
				if err != nil && !strings.Contains(err.Error(), "event already exists") {
					return err
				}

				if log.ExtraData != nil {
					var extraData relay.ExtraData
					err = json.Unmarshal(*log.ExtraData, &extraData)
					if err != nil {
						return err
					}

					nostrethMention, err := nostreth.CreateQuoteRepostEvent(extraData.Description, group, sev, n.RelayUrl)
					if err != nil {
						return err
					}

					_, err = n.SignAndSaveEvent(ctx, nostrethMention)
					if err != nil && !strings.Contains(err.Error(), "event already exists") {
						return err
					}
				}
			}

			if len(logs) == 0 {
				break
			}

			offset += len(logs)
			log.Printf("Migrated %d logs", offset)
		}

	}
	return nil
}

func convertLogToEvent(log *nostreth.Log) *nostr.Event {
	ev, err := nostreth.CreateTxLogEvent(*log)
	if err != nil {
		fmt.Println("Error creating tx log event:", err)
		return nil
	}

	return ev
}
