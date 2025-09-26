package ethrequest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

const (
	ETHEstimateGas        = "eth_estimateGas"
	ETHSendRawTransaction = "eth_sendRawTransaction"
	ETHSign               = "eth_sign"
	ETHChainID            = "eth_chainId"
)

type EthBlock struct {
	Number    string `json:"number"`
	Timestamp string `json:"timestamp"`
}

type EthService struct {
	rpc    *rpc.Client
	client *ethclient.Client
	ctx    context.Context
}

func (e *EthService) Context() context.Context {
	return e.ctx
}

func NewEthService(ctx context.Context, endpoint string) (*EthService, error) {
	rpc, err := rpc.Dial(endpoint)
	if err != nil {
		return nil, err
	}

	client := ethclient.NewClient(rpc)

	return &EthService{rpc, client, ctx}, nil
}

func (e *EthService) Close() {
	e.client.Close()
}

func (e *EthService) BlockTime(number *big.Int) (uint64, error) {
	// Some blockchains have a slightly different format than Ethereum Blocks, so we need to use a custom Block struct
	var blk *EthBlock
	err := e.rpc.Call(&blk, "eth_getBlockByNumber", fmt.Sprintf("0x%s", number.Text(16)), true)
	if err != nil {
		return 0, err
	}

	if blk == nil {
		return 0, errors.New("block not found")
	}

	v, err := hexutil.DecodeUint64(blk.Timestamp)
	if err != nil {
		return 0, err
	}

	return v, nil
}

func (e *EthService) Backend() bind.ContractBackend {
	return e.client
}

func (e *EthService) CallContract(call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	return e.client.CallContract(e.ctx, call, blockNumber)
}

func (e *EthService) ListenForLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- types.Log) error {
	for {
		sub, err := e.client.SubscribeFilterLogs(ctx, q, ch)
		if err != nil {
			log.Default().Println("error subscribing to logs", err.Error())

			<-time.After(1 * time.Second)

			continue
		}

		select {
		case <-ctx.Done():
			log.Default().Println("context done, unsubscribing")
			sub.Unsubscribe()

			return ctx.Err()
		case err := <-sub.Err():
			// subscription error, try and re-subscribe
			log.Default().Println("subscription error", err.Error())
			sub.Unsubscribe()

			<-time.After(1 * time.Second)

			continue
		}
	}
}

func (e *EthService) CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error) {
	return e.client.CodeAt(e.ctx, account, blockNumber)
}

func (e *EthService) NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error) {
	return e.client.NonceAt(e.ctx, account, blockNumber)
}

func (e *EthService) BaseFee() (*big.Int, error) {
	// Get the latest block header
	header, err := e.client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	return header.BaseFee, nil
}

func (e *EthService) EstimateGasPrice() (*big.Int, error) {
	return e.client.SuggestGasPrice(e.ctx)
}

func (e *EthService) EstimateGasLimit(msg ethereum.CallMsg) (uint64, error) {
	gasLimit, err := e.client.EstimateGas(e.ctx, msg)
	if err != nil {
		// Log more details about the error
		fmt.Printf("EstimateGasLimit error type: %T\n", err)
		fmt.Printf("EstimateGasLimit error details: %+v\n", err)

		// Try to extract more information if it's an RPC error
		if rpcErr, ok := err.(rpc.Error); ok {
			fmt.Printf("RPC error code: %d\n", rpcErr.ErrorCode())
			fmt.Printf("RPC error message: %s\n", rpcErr.Error())
		}
	}
	return gasLimit, err
}

func (e *EthService) NewTx(nonce uint64, from, to common.Address, data []byte, extraGas int) (*types.Transaction, error) {
	baseFee, err := e.BaseFee()
	if err != nil {
		return nil, fmt.Errorf("error getting base fee: %w", err)
	}

	// Set the priority fee per gas (miner tip)
	tip, err := e.MaxPriorityFeePerGas()
	if err != nil {
		return nil, fmt.Errorf("error getting max priority fee: %w", err)
	}

	// Adaptive gas pricing based on network conditions
	// If base fee is very low (< 0.01 Gwei), this is likely a low-cost network like Base
	// If base fee is higher, use more conservative pricing
	lowCostNetworkThreshold := big.NewInt(10000000) // 0.01 Gwei in wei

	var minPriorityFee *big.Int
	var baseFeeMultiplier *big.Int
	var gasBufferPercent uint64

	if baseFee.Cmp(lowCostNetworkThreshold) < 0 {
		// Low-cost network (like Base): Use minimal fees
		minPriorityFee = big.NewInt(1000000) // 0.001 Gwei
		baseFeeMultiplier = big.NewInt(1)    // No multiplier
		gasBufferPercent = 10                // 10% buffer
	} else {
		// Higher-cost network: Use more conservative pricing
		minPriorityFee = big.NewInt(1000000000) // 1 Gwei
		baseFeeMultiplier = big.NewInt(2)       // 2x multiplier for safety
		gasBufferPercent = 50                   // 50% buffer for safety
	}

	// Apply minimum priority fee
	if tip.Cmp(minPriorityFee) < 0 {
		tip = minPriorityFee
	}

	// Calculate max priority fee per gas with adaptive buffer
	var maxPriorityFeePerGas *big.Int
	if baseFee.Cmp(lowCostNetworkThreshold) < 0 {
		// Low-cost network: Use tip directly (no additional buffer)
		maxPriorityFeePerGas = tip
	} else {
		// Higher-cost network: Add 10% buffer for safety
		buffer := new(big.Int).Div(tip, big.NewInt(10))
		maxPriorityFeePerGas = new(big.Int).Add(tip, buffer)
	}

	// Calculate max fee per gas
	maxFeePerGas := new(big.Int).Add(maxPriorityFeePerGas, new(big.Int).Mul(baseFee, baseFeeMultiplier))

	// Prepare the call message
	msg := ethereum.CallMsg{
		From:     from, // the account executing the function
		To:       &to,
		Gas:      0,    // set to 0 for estimation
		GasPrice: nil,  // set to nil for estimation
		Value:    nil,  // set to nil for estimation
		Data:     data, // the function call data
	}

	gasLimit, err := e.EstimateGasLimit(msg)
	if err != nil {
		return nil, fmt.Errorf("gas estimation failed: %w", err)
	}

	// Calculate gas buffer based on network conditions
	var gasBuffer uint64
	if baseFee.Cmp(lowCostNetworkThreshold) < 0 {
		// Low-cost network: Use more conservative buffer to prevent failures
		// Use 20% buffer or minimum 20k gas, whichever is higher
		gasBuffer = gasLimit / 2 // 50% buffer
		if gasBuffer < 20000 {
			gasBuffer = 20000 // minimum 20k gas buffer
			if gasBuffer < gasLimit/20 {
				gasBuffer = gasLimit / 20 // At least 5% buffer
			}
		}
	} else {
		// Higher-cost network: Use percentage-based buffer
		gasBuffer = gasLimit * gasBufferPercent / 100
	}

	// Add small buffers to fee caps
	gasFeeCap := new(big.Int).Add(maxFeePerGas, new(big.Int).Div(maxFeePerGas, big.NewInt(10)))
	gasTipCap := new(big.Int).Add(maxPriorityFeePerGas, new(big.Int).Div(maxPriorityFeePerGas, big.NewInt(10)))

	if extraGas > 0 {
		gasFeeCap = new(big.Int).Add(maxFeePerGas, new(big.Int).Mul(maxFeePerGas, big.NewInt(int64(extraGas))))
		gasTipCap = new(big.Int).Add(maxPriorityFeePerGas, new(big.Int).Mul(maxPriorityFeePerGas, big.NewInt(int64(extraGas))))
	}

	// Create a new dynamic fee transaction
	tx := types.NewTx(&types.DynamicFeeTx{
		Nonce:     nonce,
		GasFeeCap: gasFeeCap,
		GasTipCap: gasTipCap,
		Gas:       gasLimit + gasBuffer,
		To:        &to,
		Value:     common.Big0,
		Data:      data,
	})
	return tx, nil
}

func (e *EthService) EstimateFullGas(from common.Address, tx *types.Transaction) (uint64, error) {

	msg := ethereum.CallMsg{
		From:       from,
		To:         tx.To(),
		Gas:        tx.Gas(),
		GasPrice:   tx.GasPrice(),
		GasFeeCap:  tx.GasFeeCap(),
		GasTipCap:  tx.GasTipCap(),
		Value:      tx.Value(),
		Data:       tx.Data(),
		AccessList: tx.AccessList(),
	}

	return e.client.EstimateGas(e.ctx, msg)
}

func (e *EthService) SendTransaction(tx *types.Transaction) error {
	return e.client.SendTransaction(e.ctx, tx)
}

func (e *EthService) MaxPriorityFeePerGas() (*big.Int, error) {
	var hexFee string
	err := e.rpc.Call(&hexFee, "eth_maxPriorityFeePerGas")
	if err != nil {
		return common.Big0, err
	}

	fee := new(big.Int)
	_, ok := fee.SetString(hexFee[2:], 16) // remove the "0x" prefix and parse as base 16
	if !ok {
		return nil, errors.New("invalid hex string")
	}

	return fee, nil
}

func (e *EthService) StorageAt(addr common.Address, slot common.Hash) ([]byte, error) {
	return e.client.StorageAt(e.ctx, addr, slot, nil)
}

func (e *EthService) ChainID() (*big.Int, error) {
	chid, err := e.client.ChainID(e.ctx)
	if err != nil {
		return nil, err
	}

	return chid, nil
}

func (e *EthService) Call(method string, result any, params json.RawMessage) error {
	var args []any

	if err := json.Unmarshal(params, &args); err != nil {
		return fmt.Errorf("failed to unmarshal request body: %w", err)
	}

	return e.client.Client().Call(result, method, args...)
}

func (e *EthService) LatestBlock() (*big.Int, error) {
	var blk *EthBlock
	err := e.rpc.Call(&blk, "eth_getBlockByNumber", "latest", true)
	if err != nil {
		return common.Big0, err
	}

	v, err := hexutil.DecodeBig(blk.Number)
	if err != nil {
		return common.Big0, err
	}
	return v, nil
}

func (e *EthService) FilterLogs(q ethereum.FilterQuery) ([]types.Log, error) {
	return e.client.FilterLogs(e.ctx, q)
}

func (e *EthService) WaitForTx(tx *types.Transaction, timeout int) error {
	// Create a context that will be canceled after 4 seconds
	ctx, cancel := context.WithTimeout(e.ctx, time.Duration(timeout)*time.Second)
	defer cancel() // Cancel the context when the function returns

	rcpt, err := bind.WaitMined(ctx, e.client, tx)
	if err != nil {
		return err
	}

	if rcpt.Status != types.ReceiptStatusSuccessful {
		return errors.New("tx failed")
	}

	return nil
}
