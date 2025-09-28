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
	"github.com/comunifi/relay/internal/nostr"
	"github.com/comunifi/relay/internal/ws"
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
	n          *nostr.Nostr
	evm        relay.EVMRequester
	pushq      *Service
	pools      *ws.ConnectionPools
}

func NewUserOpService(ctx context.Context, chainID *big.Int, db *db.DB, n *nostr.Nostr,
	evm relay.EVMRequester,
	pushq *Service,
	pools *ws.ConnectionPools) *UserOpService {
	return &UserOpService{
		ctx:        ctx,
		inProgress: map[common.Address][]string{},
		chainID:    chainID,
		db:         db,
		n:          n,
		evm:        evm,
		pushq:      pushq,
		pools:      pools,
	}
}

// Process method processes messages of type []relay.Message and returns processed messages and an errors if any.
func (s *UserOpService) Process(messages []relay.Message) (invalid []relay.Message, errors []error) {
	invalid = []relay.Message{}
	errors = []error{}

	messagesBySponsor := map[common.Address][]relay.Message{}
	txmBySponsor := map[common.Address][]relay.UserOpMessage{}

	// first organize messages by sponsors
	for _, message := range messages {
		// Type assertion to check if the msgs... is of type relay.UserOpMessage
		txm, ok := message.Message.(relay.UserOpMessage)
		if !ok {
			// If the message is not of type relay.UserOpMessage, return an error
			invalid = append(invalid, message)
			errors = append(errors, fmt.Errorf("invalid tx msgs..."))
			continue
		}

		// Fetch the sponsor's corresponding private key from the database
		sponsorKey, err := s.db.SponsorDB.GetSponsor(txm.Paymaster.Hex())
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
		txmBySponsor[sponsor] = append(txmBySponsor[sponsor], txm)
	}

	// go through each sponsor and process the messages
	for sponsor, txms := range txmBySponsor {
		sampleTxm := txms[0] // use the first txm to get information we need to process the messages
		msgs := messagesBySponsor[sponsor]

		// Fetch the sponsor's corresponding private key from the database
		sponsorKey, err := s.db.SponsorDB.GetSponsor(sampleTxm.Paymaster.Hex())
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

		ops := []tokenEntryPoint.UserOperation{}

		for _, txm := range txms {
			ops = append(ops, tokenEntryPoint.UserOperation(txm.UserOp))
		}

		// Pack the function name and arguments into calldata
		data, err := parsedABI.Pack("handleOps", ops, sampleTxm.EntryPoint)
		if err != nil {
			invalid = append(invalid, msgs...)
			for range msgs {
				errors = append(errors, err)
			}
			continue
		}

		// Create a new transaction
		tx, err := s.evm.NewTx(nonce, sponsor, sampleTxm.EntryPoint, data, sampleTxm.BumpGas)
		if err != nil {
			invalid = append(invalid, msgs...)
			for range msgs {
				errors = append(errors, err)
			}
			continue
		}

		// Sign the transaction
		signedTx, err := types.SignTx(tx, types.NewLondonSigner(sampleTxm.ChainId), privateKey)
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

		for _, txm := range txms {
			// Detect if this user operation is a transfer using the call data

			userop := txm.UserOp
			data, ok := txm.Data.(*json.RawMessage)
			if !ok {
				data = nil
			}

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

			// handle descriptions passed as extra data in v1
			txdata, ok := txm.ExtraData.(*json.RawMessage)
			if !ok {
				// if it's invalid, set it to nil to avoid errors and corrupted json
				txdata = nil
			}

			if txdata != nil {
				err = s.db.DataDB.UpsertData(log.Hash, txdata)
				if err != nil {
					// TODO: log this error somewhere
				}
			}

			// TODO: save an updated user op event

			insertedLogs[txm.Paymaster] = append(insertedLogs[txm.Paymaster], log)
		}

		// Send the signed transaction
		err = s.evm.SendTransaction(signedTx)
		if err != nil {
			// If there's an error, check if it's an RPC error
			e, ok := err.(rpc.Error)
			if ok && e.ErrorCode() == -32010 {
				// If the error code is -32010, it means that a tx needs to be replaced
				// TODO: update user op event so it is re-submitted

				for _, msg := range msgs {
					txm, ok := msg.Message.(relay.UserOpMessage)
					if ok {
						txm.BumpGas += 1
						println("bumping gas for new message:", txm.BumpGas)
						invalid = append(invalid, *relay.NewMessage(msg.ID, txm, msg.RetryCount, msg.Response))
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
		for _, msg := range msgs {
			msg.Respond(signedTxHash, nil)
		}

		go func() {
			// async wait for the transaction to be mined
			err = s.evm.WaitForTx(signedTx, 16)
			if err != nil {
				// TODO: update user op event so it is deleted
				// TODO: log this error somewhere, submitted but then was not mined within a reasonable amount of time
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
