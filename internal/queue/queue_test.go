package queue

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/citizenapp2/relay/pkg/relay"
	"github.com/ethereum/go-ethereum/common"
)

type TestTxProcessor struct {
	t             *testing.T
	expectedCount int
	count         int

	err chan error

	expectedError error
}

func (p *TestTxProcessor) Process(messages []relay.Message) ([]relay.Message, []error) {
	invalidMessages := []relay.Message{}
	messageErrors := []error{}

	for _, m := range messages {
		p.count++
		_, ok := m.Message.(relay.UserOpMessage)
		if !ok {
			invalidMessages = append(invalidMessages, m)
			messageErrors = append(messageErrors, p.expectedError)
			p.err <- p.expectedError
		}
	}

	return invalidMessages, messageErrors
}

func TestProcessMessages(t *testing.T) {
	expectedTxError := errors.New("invalid tx message")

	t.Run("TxMessages", func(t *testing.T) {
		testCases := []relay.Message{
			*relay.NewTxMessage(common.Address{}, common.Address{}, common.Big0, relay.UserOp{}, nil, nil),
			*relay.NewTxMessage(common.Address{}, common.Address{}, common.Big0, relay.UserOp{}, nil, nil),
			*relay.NewTxMessage(common.Address{}, common.Address{}, common.Big0, relay.UserOp{}, nil, nil),
			*relay.NewTxMessage(common.Address{}, common.Address{}, common.Big0, relay.UserOp{}, nil, nil),
			*relay.NewTxMessage(common.Address{}, common.Address{}, common.Big0, relay.UserOp{}, nil, nil),
			*relay.NewTxMessage(common.Address{}, common.Address{}, common.Big0, relay.UserOp{}, nil, nil),
		}

		q, qerr := NewService("tx", 3, 10, nil)

		p := &TestTxProcessor{t, len(testCases), 0, qerr, expectedTxError}

		go func() {
			for err := range qerr {
				if strings.Contains(err.Error(), "queue is full") || strings.Contains(err.Error(), "queue is almost full") {
					continue
				}

				if err != expectedTxError {
					t.Fatalf("expected %s, got %s", expectedTxError, err)
				}
			}
		}()

		go func() {
			for _, tc := range testCases {
				q.Enqueue(tc)
			}

			for {
				if p.count >= p.expectedCount {
					break
				}

				time.Sleep(100 * time.Millisecond)
			}
			q.Close()
		}()

		err := q.Start(p)
		if err != nil {
			t.Fatal(err)
		}

		if p.count != p.expectedCount {
			t.Fatalf("expected %d, got %d", p.expectedCount, p.count)
		}
	})

	t.Run("TxMessages with 1 invalid", func(t *testing.T) {
		testCases := []relay.Message{
			*relay.NewTxMessage(common.Address{}, common.Address{}, common.Big0, relay.UserOp{}, nil, nil),
			*relay.NewTxMessage(common.Address{}, common.Address{}, common.Big0, relay.UserOp{}, nil, nil),
			*relay.NewTxMessage(common.Address{}, common.Address{}, common.Big0, relay.UserOp{}, nil, nil),
			*relay.NewTxMessage(common.Address{}, common.Address{}, common.Big0, relay.UserOp{}, nil, nil),
			{ID: "invalid", CreatedAt: time.Now(), RetryCount: 0, Message: "invalid"},
			*relay.NewTxMessage(common.Address{}, common.Address{}, common.Big0, relay.UserOp{}, nil, nil),
			*relay.NewTxMessage(common.Address{}, common.Address{}, common.Big0, relay.UserOp{}, nil, nil),
			*relay.NewTxMessage(common.Address{}, common.Address{}, common.Big0, relay.UserOp{}, nil, nil),
			*relay.NewTxMessage(common.Address{}, common.Address{}, common.Big0, relay.UserOp{}, nil, nil),
			*relay.NewTxMessage(common.Address{}, common.Address{}, common.Big0, relay.UserOp{}, nil, nil),
			*relay.NewTxMessage(common.Address{}, common.Address{}, common.Big0, relay.UserOp{}, nil, nil),
		}

		q, qerr := NewService("tx", 3, 10, nil)

		p := &TestTxProcessor{t, len(testCases) + 3, 0, qerr, expectedTxError}

		go func() {
			for err := range qerr {
				if strings.Contains(err.Error(), "queue is full") || strings.Contains(err.Error(), "queue is almost full") {
					continue
				}

				if err != expectedTxError {
					t.Fatalf("expected %s, got %s", expectedTxError, err)
				}
			}
		}()

		go func() {
			for _, tc := range testCases {
				q.Enqueue(tc)
			}

			for {
				if p.count >= p.expectedCount {
					break
				}

				time.Sleep(100 * time.Millisecond)
			}
			q.Close()
		}()

		err := q.Start(p)
		if err != nil {
			t.Fatal(err)
		}

		if p.count != p.expectedCount {
			t.Fatalf("expected %d, got %d", p.expectedCount, p.count)
		}
	})

	t.Run("Push Notifications", func(t *testing.T) {
		// TODO: implement
	})
}
