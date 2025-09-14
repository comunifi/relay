package chain

import (
	"encoding/json"
	"math/big"
	"net/http"

	"github.com/citizenapp2/relay/pkg/relay"
)

type Service struct {
	evm     relay.EVMRequester
	chainId *big.Int
}

// NewService
func NewService(evm relay.EVMRequester, chid *big.Int) *Service {
	return &Service{
		evm,
		chid,
	}
}

func (s *Service) ChainId(r *http.Request) (any, error) {
	// Return the message ID
	return s.chainId.String(), nil
}

func (s *Service) EthCall(r *http.Request) (any, error) {

	var params json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		return nil, err
	}

	var result any
	err := s.evm.Call("eth_call", &result, params)
	if err != nil {
		println(err.Error())
		return nil, err
	}

	return result, nil
}

func (s *Service) EthBlockNumber(r *http.Request) (any, error) {

	var params json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		return nil, err
	}

	var result any
	err := s.evm.Call("eth_blockNumber", &result, params)
	if err != nil {
		println(err.Error())
		return nil, err
	}

	return result, nil
}

func (s *Service) EthGetBlockByNumber(r *http.Request) (any, error) {

	var params json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		return nil, err
	}

	var result any
	err := s.evm.Call("eth_getBlockByNumber", &result, params)
	if err != nil {
		println(err.Error())
		return nil, err
	}

	return result, nil
}

func (s *Service) EthMaxPriorityFeePerGas(r *http.Request) (any, error) {

	var params json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		return nil, err
	}

	var result any
	err := s.evm.Call("eth_maxPriorityFeePerGas", &result, params)
	if err != nil {
		println(err.Error())
		return nil, err
	}

	return result, nil

}

func (s *Service) EthGetTransactionReceipt(r *http.Request) (any, error) {

	var params json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		return nil, err
	}

	var result any
	err := s.evm.Call("eth_getTransactionReceipt", &result, params)
	if err != nil {
		println(err.Error())
		return nil, err
	}

	return result, nil
}

func (s *Service) EthGetTransactionCount(r *http.Request) (any, error) {

	var params json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		return nil, err
	}

	var result any
	err := s.evm.Call("eth_getTransactionCount", &result, params)
	if err != nil {
		println(err.Error())
		return nil, err
	}

	return result, nil
}

func (s *Service) EthEstimateGas(r *http.Request) (any, error) {

	var params json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		return nil, err
	}

	var result any
	err := s.evm.Call("eth_estimateGas", &result, params)
	if err != nil {
		println(err.Error())
		return nil, err
	}

	return result, nil
}

func (s *Service) EthGasPrice(r *http.Request) (any, error) {

	var params json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		return nil, err
	}

	var result any
	err := s.evm.Call("eth_gasPrice", &result, params)
	if err != nil {
		println(err.Error())
		return nil, err
	}

	return result, nil
}

func (s *Service) EthSendRawTransaction(r *http.Request) (any, error) {

	var params json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		return nil, err
	}

	var result any
	err := s.evm.Call("eth_sendRawTransaction", &result, params)
	if err != nil {
		println(err.Error())
		return nil, err
	}

	return result, nil
}
