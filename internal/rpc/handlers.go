package rpc

import (
	"net/http"

	"github.com/citizenapp2/relay/internal/ws"
)

type Handlers struct {
	Manager *ws.ConnectionPool
}

func NewHandlers() *Handlers {
	return &Handlers{
		Manager: ws.NewConnectionPool("rpc"),
	}
}

func (h *Handlers) HandleConnection(w http.ResponseWriter, r *http.Request) {
	h.Manager.Connect(w, r)
}
