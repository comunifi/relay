package accounts

import (
	"context"
	"net/http"

	"github.com/comunifi/relay/internal/db"
	com "github.com/comunifi/relay/pkg/common"
	"github.com/comunifi/relay/pkg/relay"

	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"
)

type Service struct {
	evm relay.EVMRequester

	db *db.DB
}

func NewService(evm relay.EVMRequester, db *db.DB) *Service {
	return &Service{
		evm: evm,
		db:  db,
	}
}

// Create handler for publishing an account
func (s *Service) Exists(w http.ResponseWriter, r *http.Request) {
	accaddr := chi.URLParam(r, "acc_addr")

	acc := common.HexToAddress(accaddr)

	// Get the contract's bytecode
	bytecode, err := s.evm.CodeAt(context.Background(), acc, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Check if the account contract is already deployed
	if len(bytecode) == 0 {
		http.Error(w, "account contract does not exist", http.StatusNotFound)
		return
	}

	err = com.Body(w, nil, nil)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}
