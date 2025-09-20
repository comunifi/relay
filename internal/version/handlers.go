package version

import (
	"net/http"

	"github.com/comunifi/relay/pkg/common"
	"github.com/comunifi/relay/pkg/relay"
)

type Service struct{}

func NewService() *Service {
	return &Service{}
}

type response struct {
	Version string `json:"version"`
}

// Current returns the current version of the API
func (s *Service) Current(w http.ResponseWriter, r *http.Request) {
	err := common.Body(w, &response{Version: relay.Version}, nil)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}
