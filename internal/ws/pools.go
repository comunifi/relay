package ws

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/citizenapp2/relay/pkg/relay"
)

type ConnectionPools struct {
	pools map[string]*ConnectionPool
	mu    sync.Mutex
}

func NewConnectionPools() *ConnectionPools {
	return &ConnectionPools{
		pools: make(map[string]*ConnectionPool),
	}
}

// Connect connects a client to a topic or creates a new topic
func (p *ConnectionPools) Connect(w http.ResponseWriter, r *http.Request, topic string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.pools[topic]; !ok || !p.pools[topic].IsOpen() {
		p.pools[topic] = NewConnectionPool(topic)

		go p.pools[topic].Run()
	}

	p.pools[topic].Connect(w, r)
}

// BroadcastMessage broadcasts a message to all clients in a topic
func (p *ConnectionPools) BroadcastMessage(t relay.WSMessageType, m relay.WSMessageCreator) {
	wsm := m.ToWSMessage(t)
	if wsm == nil {
		return
	}

	b, err := json.Marshal(wsm)
	if err != nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if pool, ok := p.pools[wsm.PoolID]; ok && pool.IsOpen() {
		queries := pool.Queries()
		for _, query := range queries {
			if !m.MatchesQuery(query) {
				continue
			}

			pool.BroadcastMessage(query, b)
		}
	}
}
