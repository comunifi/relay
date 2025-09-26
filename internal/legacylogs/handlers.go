package legacylogs

import (
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/comunifi/relay/internal/nostr"
	com "github.com/comunifi/relay/pkg/common"
	"github.com/comunifi/relay/pkg/relay"
	"github.com/go-chi/chi/v5"
)

type Service struct {
	chainID *big.Int
	n       *nostr.Nostr

	evm relay.EVMRequester
}

func NewService(chainID *big.Int, n *nostr.Nostr, evm relay.EVMRequester) *Service {
	return &Service{
		chainID: chainID,
		n:       n,
		evm:     evm,
	}
}

func (s *Service) GetSingle(w http.ResponseWriter, r *http.Request) {
	// parse hash from url params
	hash := chi.URLParam(r, "hash")

	if hash == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	tx, err := s.n.GetLog(hash, s.chainID.String())
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	err = com.Body(w, tx, nil)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (s *Service) GetAll(w http.ResponseWriter, r *http.Request) {
	// parse contract address from url params
	contractAddr := chi.URLParam(r, "contract_address")
	if contractAddr == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// parse topic from url query
	topic := chi.URLParam(r, "topic")
	if topic == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// parse maxDate from url query
	maxDateq, _ := url.QueryUnescape(r.URL.Query().Get("maxDate"))

	t, err := time.Parse(time.RFC3339, maxDateq)
	if err != nil {
		t = time.Now()
	}
	maxDate := t.UTC()

	// parse pagination params from url query
	limitq := r.URL.Query().Get("limit")
	offsetq := r.URL.Query().Get("offset")

	limit, err := strconv.Atoi(limitq)
	if err != nil {
		limit = 20
	}

	offset, err := strconv.Atoi(offsetq)
	if err != nil {
		offset = 0
	}

	// get logs from db
	logs, err := s.n.GetAllPaginatedLogs(com.ChecksumAddress(contractAddr), topic, maxDate, limit, offset)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// TODO: remove legacy support
	total := offset + limit

	err = com.BodyMultiple(w, logs, com.Pagination{Limit: limit, Offset: offset, Total: total})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (s *Service) GetAllNew(w http.ResponseWriter, r *http.Request) {
	// parse contract address from url params
	contractAddr := chi.URLParam(r, "contract_address")
	if contractAddr == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// parse topic from url query
	topic := chi.URLParam(r, "topic")
	if topic == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// parse fromDate from url query
	fromDateq, _ := url.QueryUnescape(r.URL.Query().Get("fromDate"))

	t, err := time.Parse(time.RFC3339, fromDateq)
	if err != nil {
		t = time.Now()
	}
	fromDate := t.UTC()

	// parse pagination params from url query
	limitq := r.URL.Query().Get("limit")
	offsetq := r.URL.Query().Get("offset")

	limit, err := strconv.Atoi(limitq)
	if err != nil {
		limit = 20
	}

	offset, err := strconv.Atoi(offsetq)
	if err != nil {
		offset = 0
	}

	// get logs from db
	logs, err := s.n.GetAllNewLogs(com.ChecksumAddress(contractAddr), topic, fromDate, limit, offset)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// TODO: remove legacy support
	total := offset + limit

	err = com.BodyMultiple(w, logs, com.Pagination{Limit: limit, Offset: offset, Total: total})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

// Get godoc
//
//		@Summary		Fetch transfer logs
//		@Description	get transfer logs for a given token and account
//		@Tags			logs
//		@Accept			json
//		@Produce		json
//		@Param			contract_address	path		string	true	"Token Contract Address"
//	 	@Param			acc_address	path		string	true	"Address of the account"
//		@Success		200	{object}	common.Response
//		@Failure		400
//		@Failure		404
//		@Failure		500
//		@Router			/logs/transfers/{contract_address}/{acc_addr} [get]
func (s *Service) Get(w http.ResponseWriter, r *http.Request) {
	// parse contract address from url params
	contractAddr := chi.URLParam(r, "contract_address")
	if contractAddr == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// parse topic from url query
	topic := chi.URLParam(r, "topic")
	if topic == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// parse maxDate from url query
	maxDateq, _ := url.QueryUnescape(r.URL.Query().Get("maxDate"))

	t, err := time.Parse(time.RFC3339, maxDateq)
	if err != nil {
		t = time.Now()
	}
	maxDate := t.UTC()

	// parse pagination params from url query
	limitq := r.URL.Query().Get("limit")
	offsetq := r.URL.Query().Get("offset")

	limit, err := strconv.Atoi(limitq)
	if err != nil {
		limit = 20
	}

	offset, err := strconv.Atoi(offsetq)
	if err != nil {
		offset = 0
	}

	dataFilters := relay.ParseJSONBFilters(r.URL.Query(), "data")

	dataFilters2 := relay.ParseJSONBFilters(r.URL.Query(), "data2")

	// get logs from db
	logs, err := s.n.GetPaginatedLogs(com.ChecksumAddress(contractAddr), topic, maxDate, dataFilters, dataFilters2, limit, offset) // TODO: add topics
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// TODO: remove legacy support
	total := offset + limit

	err = com.BodyMultiple(w, logs, com.Pagination{Limit: limit, Offset: offset, Total: total})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (s *Service) GetNew(w http.ResponseWriter, r *http.Request) {
	// parse contract address from url params
	contractAddr := chi.URLParam(r, "contract_address")
	if contractAddr == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// parse topic from url query
	topic := chi.URLParam(r, "topic")
	if topic == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// parse fromDate from url query
	fromDateq, _ := url.QueryUnescape(r.URL.Query().Get("fromDate"))

	t, err := time.Parse(time.RFC3339, fromDateq)
	if err != nil {
		t = time.Now()
	}
	fromDate := t.UTC()

	// parse pagination params from url query
	limitq := r.URL.Query().Get("limit")
	offsetq := r.URL.Query().Get("offset")

	limit, err := strconv.Atoi(limitq)
	if err != nil {
		limit = 20
	}

	offset, err := strconv.Atoi(offsetq)
	if err != nil {
		offset = 0
	}

	dataFilters := relay.ParseJSONBFilters(r.URL.Query(), "data")

	dataFilters2 := relay.ParseJSONBFilters(r.URL.Query(), "data2")

	// get logs from db
	logs, err := s.n.GetNewLogs(com.ChecksumAddress(contractAddr), topic, fromDate, dataFilters, dataFilters2, limit, offset)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// TODO: remove legacy support
	total := offset + limit

	err = com.BodyMultiple(w, logs, com.Pagination{Limit: limit, Offset: offset, Total: total})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}
