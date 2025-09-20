package indexer

import (
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	nostreth "github.com/comunifi/nostr-eth"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

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
			CreatedAt: time.Unix(int64(blk.Time), 0).UTC(),
			UpdatedAt: time.Now().UTC(),
			Nonce:     int64(0),
			To:        log.Address.Hex(),
			Value:     big.NewInt(0), // Set to 0 as we don't have this information from the log
			Data:      (*json.RawMessage)(&b),
		}

		l.Hash = l.GenerateUniqueHash()

		ev, err := nostreth.CreateTxLogEvent(*l)
		if err != nil {
			fmt.Println("Error creating tx log event:", err)
			return nil
		}

		err = ev.Sign(i.secretKey)
		if err != nil {
			return err
		}

		err = i.ndb.SaveEvent(i.ctx, ev)
		if err != nil {
			return err
		}

		// i.pools.BroadcastMessage(relay.WSMessageTypeUpdate, l)
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
