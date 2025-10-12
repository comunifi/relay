package indexer

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	nostreth "github.com/comunifi/nostr-eth"
	"github.com/nbd-wtf/go-nostr"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/jackc/pgx/v5"

	comm "github.com/comunifi/relay/pkg/common"
	"github.com/comunifi/relay/pkg/relay"
)

type block struct {
	Number uint64
	Time   uint64
}

type cleanup struct {
	t uint64
	b uint64
}

func (i *Indexer) ListenToLogs(ev *relay.Event, quitAck chan error) error {
	logch := make(chan types.Log)

	q, err := i.FilterQueryFromEvent(ev)
	if err != nil {
		return err
	}

	go func() {
		err := i.evm.ListenForLogs(i.ctx, *q, logch)
		if err != nil {
			quitAck <- err
		}
	}()

	blks := map[uint64]*block{}
	var toDelete []cleanup

	for log := range logch {
		blk, ok := blks[log.BlockNumber]
		if !ok {
			t, err := i.evm.BlockTime(big.NewInt(int64(log.BlockNumber)))
			if err != nil {
				return err
			}

			blk = &block{Number: log.BlockNumber, Time: t}
			blks[log.BlockNumber] = blk

			// clean up old blocks
			for _, v := range toDelete {
				if v.t < t {
					delete(blks, v.b)
					toDelete = comm.Filter(toDelete, func(c cleanup) bool { return c.b != v.b })
				}
			}

			// set to cleanup block after 60 seconds
			toDelete = append(toDelete, cleanup{t: blk.Time + 60, b: blk.Number})
		}

		topics, err := relay.ParseTopicsFromHashes(ev, log.Topics, log.Data)
		if err != nil {
			// Log the error but don't crash the indexer
			// This can happen when event signatures are malformed or empty
			fmt.Printf("[%s] warning: failed to parse topics from log: %v\n", ev.Contract, err)
			continue
		}

		b, err := topics.MarshalJSON()
		if err != nil {
			return err
		}

		l := &nostreth.Log{
			TxHash:    log.TxHash.Hex(),
			ChainID:   i.chainID.String(),
			Topic:     ev.Topic,
			CreatedAt: time.Unix(int64(blk.Time), 0).UTC(),
			UpdatedAt: time.Now().UTC(),
			Nonce:     int64(0),
			To:        log.Address.Hex(),
			Value:     big.NewInt(0), // Set to 0 as we don't have this information from the log
			Data:      (*json.RawMessage)(&b),
		}

		l.Hash = l.GenerateUniqueHash()

		var txEv *nostr.Event
		switch ev.Topic {
		case nostreth.TopicERC20Transfer:
			txEv, err = nostreth.CreateTxTransferEvent(*l)
			if err != nil {
				fmt.Println("Error creating tx log event:", err)
				return err
			}

		default:
			txEv, err = nostreth.CreateTxLogEvent(*l)
			if err != nil {
				fmt.Println("Error creating tx log event:", err)
				return err
			}
		}

		if txEv == nil {
			return errors.New("something went wrong parsing an event from a log")
		}

		txEv, err = i.n.SignAndSaveEvent(i.ctx, txEv)
		if err != nil {
			return err
		}

		txData, err := i.db.DataDB.GetData(l.Hash)
		if err != nil && err != pgx.ErrNoRows {
			return err
		}

		if txData != nil {
			// unmarshal the extra data
			var extraData relay.ExtraData
			err = json.Unmarshal(*txData, &extraData)
			if err != nil {
				return err
			}

			rev, err := nostreth.CreateQuoteRepostEvent(extraData.Description, &ev.Alias, txEv, i.n.RelayUrl)
			if err != nil {
				return err
			}

			if extraData.Description != "" {
				rev, err = i.n.SignAndSaveEvent(i.ctx, rev)
				if err != nil {
					return err
				}
			}

			err = i.db.DataDB.DeleteData(l.Hash)
			if err != nil && err != pgx.ErrNoRows {
				return err
			}
		}

		llog := &relay.LegacyLog{
			Hash:      l.Hash,
			TxHash:    l.TxHash,
			CreatedAt: l.CreatedAt,
			UpdatedAt: l.UpdatedAt,
			Nonce:     l.Nonce,
			Sender:    l.Sender,
			To:        l.To,
			Value:     l.Value,
			Data:      l.Data,
			Status:    relay.LegacyLogStatusSuccess,
			ExtraData: txData,
		}

		llog.GenerateUniqueHash(i.chainID.String())

		i.pools.BroadcastMessage(relay.WSMessageTypeUpdate, llog)
	}

	return nil
}

func (i *Indexer) FilterQueryFromEvent(ev *relay.Event) (*ethereum.FilterQuery, error) {
	topic0 := ev.GetTopic0FromEventSignature()

	topics := [][]common.Hash{
		{topic0},
	}

	// Calculate the starting block for the filter query
	// It's the last block that was indexed plus one
	currentBlock, err := i.evm.LatestBlock()
	if err != nil {
		return nil, err
	}

	fromBlock := currentBlock.Add(currentBlock, big.NewInt(1))

	contractAddr := common.HexToAddress(ev.Contract)

	return &ethereum.FilterQuery{
		FromBlock: fromBlock,
		Addresses: []common.Address{contractAddr},
		Topics:    topics,
	}, nil
}
