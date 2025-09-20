package api

import (
	"fmt"
	"log"
	"math/big"
	"net/http"

	"github.com/comunifi/relay/internal/db"
	"github.com/comunifi/relay/internal/queue"
	"github.com/comunifi/relay/internal/ws"
	"github.com/comunifi/relay/pkg/relay"
)

type Server struct {
	chainID     *big.Int
	db          *db.DB
	evm         relay.EVMRequester
	userOpQueue *queue.Service
	pools       *ws.ConnectionPools
}

func NewServer(chainID *big.Int, db *db.DB, evm relay.EVMRequester, userOpQueue *queue.Service, pools *ws.ConnectionPools) *Server {
	return &Server{chainID: chainID, db: db, evm: evm, userOpQueue: userOpQueue, pools: pools}
}

func (s *Server) Start(port int, handler http.Handler) error {
	// start the server
	log.Printf("API server starting on :%v", port)
	return http.ListenAndServe(fmt.Sprintf(":%v", port), handler)
}

func (s *Server) Stop() {

}
