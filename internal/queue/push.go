package queue

import "github.com/citizenapp2/relay/pkg/relay"

type PushService struct{}

func NewPushService() *PushService {
	return &PushService{}
}

func (p *PushService) Process(messages []relay.Message) (invalid []relay.Message, errors []error) {
	invalid = []relay.Message{}
	errors = []error{}

	println("push service processing messages", len(messages))

	return
}
