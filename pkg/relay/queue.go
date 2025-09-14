package relay

import (
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type MessageResponse struct {
	Data any
	Err  error
}

type Message struct {
	ID         string
	CreatedAt  time.Time
	RetryCount int
	Message    any
	Response   *chan MessageResponse
}

func (m *Message) Respond(data any, err error) {
	if m.Response == nil {
		return
	}

	// Try to send on channel, recover from panic if channel is closed
	defer func(me *Message) {
		if r := recover(); r != nil {
			// Channel was closed, ignore the panic
			me.Response = nil
		}
	}(m)

	*m.Response <- MessageResponse{
		Data: data,
		Err:  err,
	}
}

func (m *Message) WaitForResponse() (any, error) {
	defer m.Close()

	select {
	case resp, ok := <-*m.Response:
		if !ok {
			return nil, fmt.Errorf("response channel is closed")
		}
		// handle response
		if resp.Err != nil {
			return nil, resp.Err
		}

		return resp.Data, nil
	case <-time.After(time.Second * 12): // timeout so that we don't block the request forever in case the queue is stuck
		return nil, fmt.Errorf("request timeout")
	}
}

func (m *Message) Close() {
	if m.Response == nil {
		return
	}

	close(*m.Response)
}

type UserOpMessage struct {
	Paymaster  common.Address
	EntryPoint common.Address
	ChainId    *big.Int
	UserOp     UserOp
	Data       any
	ExtraData  any
	BumpGas    int
}

func NewMessage(id string, message any, retryCount int, response *chan MessageResponse) *Message {
	return &Message{
		ID:         id,
		CreatedAt:  time.Now(),
		RetryCount: retryCount,
		Message:    message,
		Response:   response,
	}
}

func NewTxMessage(pm, entrypoint common.Address, chainId *big.Int, userop UserOp, data, xdata *json.RawMessage) *Message {
	op := UserOpMessage{
		Paymaster:  pm,
		EntryPoint: entrypoint,
		ChainId:    chainId,
		UserOp:     userop,
		Data:       data,
		ExtraData:  xdata,
	}

	respch := make(chan MessageResponse)
	return NewMessage(common.Bytes2Hex(userop.Signature), op, 0, &respch)
}
