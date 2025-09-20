package userop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"time"

	pay "github.com/citizenwallet/smartcontracts/pkg/contracts/paymaster"
	"github.com/comunifi/relay/internal/db"
	"github.com/comunifi/relay/internal/queue"
	comm "github.com/comunifi/relay/pkg/common"
	"github.com/comunifi/relay/pkg/relay"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/go-chi/chi/v5"
)

type Service struct {
	evm     relay.EVMRequester
	db      *db.DB
	useropq *queue.Service
	chainId *big.Int
}

// NewService
func NewService(evm relay.EVMRequester, db *db.DB, useropq *queue.Service, chid *big.Int) *Service {
	return &Service{
		evm,
		db,
		useropq,
		chid,
	}
}

func (s *Service) Send(r *http.Request) (any, error) {
	// parse contract address from url params
	contractAddr := chi.URLParam(r, "pm_address")

	addr := common.HexToAddress(contractAddr)

	// Get the contract's bytecode
	bytecode, err := s.evm.CodeAt(context.Background(), addr, nil)
	if err != nil {
		return nil, err
	}

	// Check if the contract is deployed
	if len(bytecode) == 0 {
		return nil, errors.New("paymaster contract not deployed")
	}

	// instantiate paymaster contract
	pm, err := pay.NewPaymaster(addr, s.evm.Backend())
	if err != nil {
		return nil, err
	}

	// parse the incoming params

	var params []any
	err = json.NewDecoder(r.Body).Decode(&params)
	if err != nil {
		return nil, err
	}

	var userop relay.UserOp
	var epAddr string
	var data *json.RawMessage
	var xdata *json.RawMessage

	for i, param := range params {
		switch i {
		case 0:
			v, ok := param.(map[string]any)
			if !ok {
				return nil, errors.New("invalid user operation")
			}
			b, err := json.Marshal(v)
			if err != nil {
				return nil, errors.New("error marshalling user operation")
			}

			err = json.Unmarshal(b, &userop)
			if err != nil {
				return nil, errors.New("error unmarshalling user operation")
			}
		case 1:
			v, ok := param.(string)
			if !ok {
				return nil, errors.New("invalid entry point address")
			}

			epAddr = v
		case 2:
			v, ok := param.(map[string]any)
			if !ok {
				return nil, errors.New("invalid user operation")
			}

			b, err := json.Marshal(v)
			if err != nil {
				return nil, errors.New("error marshalling user operation")
			}

			data = (*json.RawMessage)(&b)
		case 3:
			v, ok := param.(map[string]any)
			if !ok {
				return nil, errors.New("invalid user operation")
			}

			b, err := json.Marshal(v)
			if err != nil {
				return nil, errors.New("error marshalling user operation")
			}

			xdata = (*json.RawMessage)(&b)
		}
	}

	if epAddr == "" {
		return nil, errors.New("error missing entry point address")
	}

	// check the paymaster signature, make sure it matches the paymaster address

	// unpack the validity and check if it is valid
	// Define the arguments
	uint48Ty, _ := abi.NewType("uint48", "uint48", nil)
	args := abi.Arguments{
		abi.Argument{
			Type: uint48Ty,
		},
		abi.Argument{
			Type: uint48Ty,
		},
	}

	// Encode the values
	validity, err := args.Unpack(userop.PaymasterAndData[20:84])
	if err != nil {
		return nil, err
	}

	validUntil, ok := validity[0].(*big.Int)
	if !ok {
		return nil, errors.New("error unmarshalling validity")
	}

	validAfter, ok := validity[1].(*big.Int)
	if !ok {
		return nil, errors.New("error unmarshalling validity")
	}

	// check if the signature is theoretically still valid
	now := time.Now().Unix()
	if validUntil.Int64() < now {
		return nil, errors.New("paymaster signature has expired")
	}

	if validAfter.Int64() > now {
		return nil, errors.New("paymaster signature is not valid yet")
	}

	// Get the hash of the message that was signed
	hash, err := pm.GetHash(nil, pay.UserOperation(userop), validUntil, validAfter)
	if err != nil {
		return nil, err
	}

	// Convert the hash to an Ethereum signed message hash
	hhash := accounts.TextHash(hash[:])

	sig := make([]byte, len(userop.PaymasterAndData[84:]))
	copy(sig, userop.PaymasterAndData[84:])

	// update the signature v to undo the 27/28 addition
	sig[crypto.RecoveryIDOffset] -= 27

	// recover the public key from the signature
	sigPublicKey, err := crypto.Ecrecover(hhash, sig)
	if err != nil {
		return nil, errors.New("error recovering public key")
	}

	// fetch the sponsor's corresponding private key from the db
	sponsorKey, err := s.db.SponsorDB.GetSponsor(addr.Hex())
	if err != nil {
		return nil, errors.New("error getting sponsor key")
	}

	// Generate ecdsa.PrivateKey from bytes
	privateKey, err := comm.HexToPrivateKey(sponsorKey.PrivateKey)
	if err != nil {
		return nil, errors.New("error converting private key")
	}

	publicKeyBytes := crypto.FromECDSAPub(&privateKey.PublicKey)

	// check if the public key matches the recovered public key
	matches := bytes.Equal(sigPublicKey, publicKeyBytes)
	if !matches {
		return nil, errors.New("paymaster signature does not match")
	}

	entryPoint := common.HexToAddress(epAddr)

	// Create a new message
	message := relay.NewTxMessage(addr, entryPoint, s.chainId, userop, data, xdata)

	// Enqueue the message
	s.useropq.Enqueue(*message)

	resp, err := message.WaitForResponse()
	if err != nil {
		return nil, err
	}

	txHash, ok := resp.(string)
	if !ok {
		return nil, errors.New("error unmarshalling tx hash")
	}

	// Return the message ID
	return txHash, nil
}
