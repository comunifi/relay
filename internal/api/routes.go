package api

import (
	"github.com/citizenapp2/relay/internal/accounts"
	"github.com/citizenapp2/relay/internal/bucket"
	"github.com/citizenapp2/relay/internal/chain"
	"github.com/citizenapp2/relay/internal/events"
	"github.com/citizenapp2/relay/internal/logs"
	"github.com/citizenapp2/relay/internal/paymaster"
	"github.com/citizenapp2/relay/internal/profiles"
	"github.com/citizenapp2/relay/internal/push"
	"github.com/citizenapp2/relay/internal/rpc"
	"github.com/citizenapp2/relay/internal/userop"
	"github.com/citizenapp2/relay/internal/version"
	"github.com/citizenapp2/relay/pkg/relay"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func (s *Server) CreateBaseRouter() *chi.Mux {
	cr := chi.NewRouter()

	return cr
}

func (s *Server) AddMiddleware(cr *chi.Mux) *chi.Mux {

	// configure middleware
	cr.Use(middleware.RequestID)
	cr.Use(middleware.Logger)

	// configure custom middleware
	cr.Use(OptionsMiddleware)
	cr.Use(HealthMiddleware)
	cr.Use(RequestSizeLimitMiddleware(10 << 20)) // Limit request bodies to 10MB
	cr.Use(middleware.Compress(9))

	return cr
}

func (s *Server) AddRoutes(cr *chi.Mux, b *bucket.Bucket) *chi.Mux {
	// instantiate handlers
	v := version.NewService()
	l := logs.NewService(s.chainID, s.db, s.evm)
	events := events.NewHandlers(s.db, s.pools)
	rpc := rpc.NewHandlers()
	pm := paymaster.NewService(s.evm, s.db)
	uop := userop.NewService(s.evm, s.db, s.userOpQueue, s.chainID)
	ch := chain.NewService(s.evm, s.chainID)
	pr := profiles.NewService(b, s.evm)
	pu := push.NewService(s.db)
	acc := accounts.NewService(s.evm, s.db)

	// configure routes
	cr.Route("/version", func(cr chi.Router) {
		cr.Get("/", v.Current)
	})

	// cr.Route("/legacy", func(cr chi.Router) {
	// 	// TODO: implement legacy routes
	// 	cr.Get("/account/{address}/exists", l.Get)
	// })

	cr.Route("/v1", func(cr chi.Router) {
		// accounts
		cr.Route("/accounts", func(cr chi.Router) {
			cr.Get("/{acc_addr}/exists", acc.Exists)
		})

		// profiles
		cr.Route("/profiles", func(cr chi.Router) {
			cr.Route("/{contract_address}", func(cr chi.Router) {
				cr.Put("/{acc_addr}", withMultiPartSignature(s.evm, pr.PinMultiPartProfile))
				cr.Patch("/{acc_addr}", withSignature(s.evm, pr.PinProfile))
				cr.Delete("/{acc_addr}", withSignature(s.evm, pr.Unpin))
			})
		})

		// push
		cr.Route("/push/{contract_address}", func(cr chi.Router) {
			cr.Put("/{acc_addr}", withSignature(s.evm, pu.AddToken))
			cr.Delete("/{acc_addr}/{token}", withSignature(s.evm, pu.RemoveAccountToken))
		})

		// logs
		cr.Route("/logs/{contract_address}", func(cr chi.Router) {
			cr.Route("/{topic}", func(cr chi.Router) {
				cr.Get("/", l.Get)
				cr.Get("/all", l.GetAll)

				cr.Get("/new", l.GetNew)
				cr.Get("/new/all", l.GetAllNew)
			})

			cr.Get("/tx/{hash}", l.GetSingle)
		})

		// rpc
		cr.Route("/rpc/{pm_address}", func(cr chi.Router) {
			cr.Post("/", withJSONRPCRequest(map[string]relay.RPCHandlerFunc{
				"pm_sponsorUserOperation":   pm.Sponsor,
				"pm_ooSponsorUserOperation": pm.OOSponsor,
				"eth_sendUserOperation":     uop.Send,
				"eth_chainId":               ch.ChainId,
				"eth_call":                  ch.EthCall,
				"eth_blockNumber":           ch.EthBlockNumber,
				"eth_getBlockByNumber":      ch.EthGetBlockByNumber,
				"eth_maxPriorityFeePerGas":  ch.EthMaxPriorityFeePerGas,
				"eth_getTransactionReceipt": ch.EthGetTransactionReceipt,
				"eth_getTransactionCount":   ch.EthGetTransactionCount,
				"eth_estimateGas":           ch.EthEstimateGas,
				"eth_gasPrice":              ch.EthGasPrice,
				"eth_sendRawTransaction":    ch.EthSendRawTransaction,
			}))
		})

		cr.Get("/events/{contract}/{topic}", events.HandleConnection) // for listening to events
		cr.Get("/rpc", rpc.HandleConnection)                          // for sending RPC calls
	})

	return cr
}
