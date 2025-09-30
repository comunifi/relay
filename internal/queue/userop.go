package queue

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/citizenwallet/smartcontracts/pkg/contracts/tokenEntryPoint"
	nostreth "github.com/comunifi/nostr-eth"
	"github.com/comunifi/relay/internal/db"
	nost "github.com/comunifi/relay/internal/nostr"
	comm "github.com/comunifi/relay/pkg/common"
	"github.com/comunifi/relay/pkg/relay"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
)

type UserOpService struct {
	ctx        context.Context
	inProgress map[common.Address][]string
	mu         sync.Mutex
	chainID    *big.Int
	db         *db.DB
	n          *nost.Nostr
	evm        relay.EVMRequester
}

func NewUserOpService(ctx context.Context, chainID *big.Int, db *db.DB, n *nost.Nostr,
	evm relay.EVMRequester) *UserOpService {
	return &UserOpService{
		ctx:        ctx,
		inProgress: map[common.Address][]string{},
		chainID:    chainID,
		db:         db,
		n:          n,
		evm:        evm,
	}
}

// Process method processes messages of type []relay.Message and returns processed messages and an errors if any.
func (s *UserOpService) Process(messages []relay.Message) (invalid []relay.Message, errors []error) {
	println("processing", len(messages), "messages")
	invalid = []relay.Message{}
	errors = []error{}

	messagesBySponsor := map[common.Address][]relay.Message{}
	opBySponsor := map[common.Address][]relay.UserOpMessage{}

	// first organize messages by sponsors
	for _, message := range messages {
		// Type assertion to check if the msgs... is of type relay.UserOpMessage
		opm, ok := message.Message.(relay.UserOpMessage)
		if !ok {
			// If the message is not of type relay.UserOpMessage, return an error
			invalid = append(invalid, message)
			errors = append(errors, fmt.Errorf("invalid tx msgs..."))
			continue
		}

		op, err := nostreth.ParseUserOpEvent(opm.Event)
		if err != nil {
			invalid = append(invalid, message)
			errors = append(errors, err)
			continue
		}

		// Fetch the sponsor's corresponding private key from the database
		sponsorKey, err := s.db.SponsorDB.GetSponsor(op.Paymaster.Hex())
		if err != nil {
			invalid = append(invalid, message)
			errors = append(errors, err)
			continue
		}

		// Generate ecdsa.PrivateKey from bytes
		privateKey, err := comm.HexToPrivateKey(sponsorKey.PrivateKey)
		if err != nil {
			invalid = append(invalid, message)
			errors = append(errors, err)
			continue
		}

		// Get the public key from the private key
		publicKey := privateKey.Public().(*ecdsa.PublicKey)

		// Convert the public key to an Ethereum address
		sponsor := crypto.PubkeyToAddress(*publicKey)

		messagesBySponsor[sponsor] = append(messagesBySponsor[sponsor], message)
		opBySponsor[sponsor] = append(opBySponsor[sponsor], opm)
	}

	// go through each sponsor and process the messages
	for sponsor, ops := range opBySponsor {
		sampleOpEvent := ops[0] // use the first txm to get information we need to process the messages
		msgs := messagesBySponsor[sponsor]

		sampleOp, err := nostreth.ParseUserOpEvent(sampleOpEvent.Event)
		if err != nil {
			invalid = append(invalid, msgs...)
			for range msgs {
				errors = append(errors, err)
			}
			continue
		}

		// Fetch the sponsor's corresponding private key from the database
		sponsorKey, err := s.db.SponsorDB.GetSponsor(sampleOp.Paymaster.Hex())
		if err != nil {
			invalid = append(invalid, msgs...)
			for range msgs {
				for range msgs {
					errors = append(errors, err)
				}
			}
			continue
		}

		// Generate ecdsa.PrivateKey from bytes
		privateKey, err := comm.HexToPrivateKey(sponsorKey.PrivateKey)
		if err != nil {
			invalid = append(invalid, msgs...)
			for range msgs {
				errors = append(errors, err)
			}
			continue
		}

		// Get the nonce for the sponsor's address
		nonce, err := s.evm.NonceAt(context.Background(), sponsor, nil)
		if err != nil {
			invalid = append(invalid, msgs...)
			for range msgs {
				errors = append(errors, err)
			}
			continue
		}

		// Get the in progress transactions for the entrypoint and increment the nonce
		inProgress := s.inProgress[sponsor]
		nonce += uint64(len(inProgress))

		// Parse the contract ABI
		parsedABI, err := tokenEntryPoint.TokenEntryPointMetaData.GetAbi()
		if err != nil {
			invalid = append(invalid, msgs...)
			for range msgs {
				errors = append(errors, err)
			}
			continue
		}

		uops := []tokenEntryPoint.UserOperation{}

		for _, op := range ops {
			uop, err := nostreth.ParseUserOpEvent(op.Event)
			if err != nil {
				invalid = append(invalid, msgs...)
				for range msgs {
					errors = append(errors, err)
				}
				continue
			}
			uops = append(uops, tokenEntryPoint.UserOperation(uop.UserOpData))
		}

		// Pack the function name and arguments into calldata
		data, err := parsedABI.Pack("handleOps", uops, sampleOp.EntryPoint)
		if err != nil {
			invalid = append(invalid, msgs...)
			for range msgs {
				errors = append(errors, err)
			}
			continue
		}

		// Create a new transaction
		tx, err := s.evm.NewTx(nonce, sponsor, *sampleOp.EntryPoint, data, sampleOp.RetryCount)
		if err != nil {
			invalid = append(invalid, msgs...)
			for range msgs {
				errors = append(errors, err)
			}
			continue
		}

		// Sign the transaction
		signedTx, err := types.SignTx(tx, types.NewLondonSigner(s.chainID), privateKey)
		if err != nil {
			invalid = append(invalid, msgs...)
			for range msgs {
				errors = append(errors, err)
			}
			continue
		}

		signedTxHash := signedTx.Hash().Hex()

		// update inProgress
		s.mu.Lock()
		s.inProgress[sponsor] = append(s.inProgress[sponsor], signedTxHash)
		s.mu.Unlock()

		insertedLogs := map[common.Address][]*nostreth.Log{}

		edb := s.db.EventDB

		events, err := edb.GetEvents(s.chainID.String())
		if err != nil {
			invalid = append(invalid, msgs...)
			for range msgs {
				errors = append(errors, err)
			}
			continue
		}

		for _, op := range ops {
			// Detect if this user operation is a transfer using the call data
			opevt, err := nostreth.ParseUserOpEvent(op.Event)
			if err != nil {
				invalid = append(invalid, msgs...)
				for range msgs {
					errors = append(errors, err)
				}
				continue
			}

			userop := opevt.UserOpData
			data := opevt.Data

			if data == nil {
				// if there is no data, it is impossible for us to generate a stable unique hash
				// so we skip it
				continue
			}

			var dataMap map[string]any
			if err := json.Unmarshal(*data, &dataMap); err != nil {
				continue
			}

			// there is data, let's check if it is valid according to any of the event signatures that we are indexing
			valid := false
			for _, event := range events {
				if event.IsValidData(dataMap) {
					// we have a match
					valid = true
					break
				}
			}

			if !valid {
				continue
			}

			// get destination address from calldata
			dest, err := comm.ParseDestinationFromCallData(userop.CallData)
			if err != nil {
				continue
			}

			log := &nostreth.Log{
				TxHash:    signedTxHash,
				ChainID:   s.chainID.String(),
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
				Nonce:     userop.Nonce.Int64(),
				Sender:    userop.Sender.Hex(),
				To:        dest.Hex(),
				Value:     common.Big0,
				Data:      data,
			}

			log.Hash = log.GenerateUniqueHash()

			// get user op message data
			txdata, ok := op.ExtraData.(*json.RawMessage)
			if !ok {
				txdata = nil
			}

			if txdata != nil {
				// we only know after submitting a transaction what the hash of the log will be
				// attach extra data to the log hash if provided
				// this allows the indexing function to post a message in nostr
				// only needed for v1 compatibility
				err = s.db.DataDB.UpsertData(log.Hash, txdata)
				if err != nil {
					// TODO: log this error somewhere
					continue
				}
			}

			println("creating user op executed event")
			ev, err := nostreth.UpdateUserOpEvent(s.chainID, userop, &signedTxHash, 0, nostreth.EventTypeUserOpExecuted, op.Event)
			if err != nil {
				// TODO: log this error somewhere
				continue
			}

			println("signing and saving user op event")
			ev, err = s.n.SignAndReplaceEvent(s.ctx, ev)
			if err != nil {
				// TODO: log this error somewhere
				continue
			}

			// TODO: save an updated user op event

			insertedLogs[*opevt.Paymaster] = append(insertedLogs[*opevt.Paymaster], log)
		}

		// Send the signed transaction
		err = s.evm.SendTransaction(signedTx)
		if err != nil {
			println("error sending transaction", err.Error())
			// If there's an error, check if it's an RPC error
			e, ok := err.(rpc.Error)
			if ok && e.ErrorCode() == -32010 {
				// If the error code is -32010, it means that a tx needs to be replaced
				// TODO: update user op event so it is re-submitted

				for _, msg := range msgs {
					opm, ok := msg.Message.(relay.UserOpMessage)
					if ok {
						opevt, err := nostreth.ParseUserOpEvent(opm.Event)
						if err != nil {
							// TODO: log this error somewhere
							continue
						}
						userop := opevt.UserOpData

						ev, err := nostreth.UpdateUserOpEvent(s.chainID, userop, &signedTxHash, opevt.RetryCount+1, nostreth.EventTypeUserOpSubmitted, opm.Event)
						if err != nil {
							// TODO: log this error somewhere
							continue
						}

						ev, err = s.n.SignAndReplaceEvent(s.ctx, ev)
						if err != nil {
							// TODO: log this error somewhere
							continue
						}

						invalid = append(invalid, msg)
					}
				}

				for range msgs {
					errors = append(errors, err)
				}

				// remove from inProgress
				s.mu.Lock()
				s.inProgress[sponsor] = comm.Filter(s.inProgress[sponsor], func(s string) bool {
					return s != signedTxHash
				})
				s.mu.Unlock()
				continue
			}
			if ok && e.ErrorCode() != -32000 {
				// If it's an RPC error and the error code is not -32000, remove the sending transfer and return the error
				// TODO: update user op event so it is deleted

				invalid = append(invalid, msgs...)
				for range msgs {
					errors = append(errors, err)
				}

				// remove from inProgress
				s.mu.Lock()
				s.inProgress[sponsor] = comm.Filter(s.inProgress[sponsor], func(s string) bool {
					return s != signedTxHash
				})
				s.mu.Unlock()
				continue
			}

			if !strings.Contains(e.Error(), "insufficient funds") {
				// If the error is not about insufficient funds, remove the sending transfer and return the error
				// TODO: update user op event so it is deleted
				// TODO: log an error, this should be resolved by an admin

				invalid = append(invalid, msgs...)
				for range msgs {
					errors = append(errors, err)
				}

				// remove from inProgress
				s.mu.Lock()
				s.inProgress[sponsor] = comm.Filter(s.inProgress[sponsor], func(s string) bool {
					return s != signedTxHash
				})
				s.mu.Unlock()
				continue
			}

			// Return the error about insufficient funds
			invalid = append(invalid, msgs...)
			for range msgs {
				errors = append(errors, err)
			}

			// remove from inProgress
			s.mu.Lock()
			s.inProgress[sponsor] = comm.Filter(s.inProgress[sponsor], func(s string) bool {
				return s != signedTxHash
			})
			s.mu.Unlock()
			continue
		}

		// v1 compatibility, responds to the messages with the tx hash
		// for _, msg := range msgs {
		// 	msg.Respond(signedTxHash, nil)
		// }

		go func() {
			// async wait for the transaction to be mined
			err = s.evm.WaitForTx(signedTx, 12)
			if err != nil {
				// TODO: log this error somewhere, submitted but then was not mined within a reasonable amount of time
				for _, op := range ops {
					opevt, err := nostreth.ParseUserOpEvent(op.Event)
					if err != nil {
						// TODO: log this error somewhere
						continue
					}
					userop := opevt.UserOpData

					ev, err := nostreth.UpdateUserOpEvent(s.chainID, userop, &signedTxHash, opevt.RetryCount, nostreth.EventTypeUserOpFailed, op.Event)
					if err != nil {
						// TODO: log this error somewhere
						continue
					}

					ev, err = s.n.SignAndReplaceEvent(s.ctx, ev)
					if err != nil {
						// TODO: log this error somewhere
						continue
					}
				}
			}

			if err == nil {
				// tx was mined
				for _, op := range ops {
					// v1 compatibility
					// clean up user op message data
					opevt, err := nostreth.ParseUserOpEvent(op.Event)
					if err != nil {
						// TODO: log this error somewhere
						continue
					}
					userop := opevt.UserOpData

					ev, err := nostreth.UpdateUserOpEvent(s.chainID, userop, &signedTxHash, opevt.RetryCount, nostreth.EventTypeUserOpConfirmed, op.Event)
					if err != nil {
						// TODO: log this error somewhere
						continue
					}

					ev, err = s.n.SignAndReplaceEvent(s.ctx, ev)
					if err != nil {
						// TODO: log this error somewhere
						continue
					}

					err = s.db.DataDB.DeleteData(fmt.Sprintf("userop:%s", userop.GetHash(s.chainID)))
					if err != nil {
						// TODO: log this error somewhere
						continue
					}
				}
			}

			// remove from inProgress
			s.mu.Lock()
			s.inProgress[sponsor] = comm.Filter(s.inProgress[sponsor], func(s string) bool {
				return s != signedTxHash
			})
			s.mu.Unlock()
		}()
	}

	return invalid, errors
}
