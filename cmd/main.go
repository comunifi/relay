package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/comunifi/relay/internal/api"
	"github.com/comunifi/relay/internal/bucket"
	"github.com/comunifi/relay/internal/config"
	"github.com/comunifi/relay/internal/db"
	"github.com/comunifi/relay/internal/ethrequest"
	"github.com/comunifi/relay/internal/indexer"
	"github.com/comunifi/relay/internal/nostr"
	"github.com/comunifi/relay/internal/queue"
	"github.com/comunifi/relay/internal/webhook"
	"github.com/comunifi/relay/internal/ws"
	"github.com/comunifi/relay/pkg/common"
	"github.com/fiatjaf/eventstore/postgresql"
	"github.com/fiatjaf/khatru"
)

func main() {
	log.Default().Println("starting relay...")

	////////////////////
	// flags
	port := flag.Int("port", 3001, "port to listen on")

	env := flag.String("env", ".env", "path to .env file")

	polling := flag.Bool("polling", false, "enable polling")

	noindex := flag.Bool("noindex", false, "disable indexing")

	useropqbf := flag.Int("buffer", 1000, "userop queue buffer size (default: 1000)")

	notify := flag.Bool("notify", false, "enable webhook notifications")

	flag.Parse()
	////////////////////

	ctx := context.Background()

	////////////////////
	// config
	conf, err := config.New(ctx, *env)
	if err != nil {
		log.Fatal(err)
	}
	////////////////////

	////////////////////
	// evm
	rpcUrl := conf.RPCURL
	if !*polling {
		log.Default().Println("running in streaming mode...")
		rpcUrl = conf.RPCWSURL
	} else {
		log.Default().Println("running in polling mode...")
	}

	evm, err := ethrequest.NewEthService(ctx, rpcUrl)
	if err != nil {
		log.Fatal(err)
	}

	chid, err := evm.ChainID()
	if err != nil {
		log.Fatal(err)
	}

	log.Default().Println("node running for chain: ", chid.String())
	////////////////////

	////////////////////
	// nostr-postgres
	log.Default().Println("starting internal db service...")

	ndb := postgresql.PostgresBackend{
		DatabaseURL: fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", conf.DBUser, conf.DBPassword, conf.DBHost, conf.DBPort, conf.DBName),
	}

	err = ndb.Init()
	if err != nil {
		log.Fatal(err)
	}
	defer ndb.Close()

	n := nostr.NewNostr(conf.RelayPrivateKey, &ndb, conf.RelayUrl)
	////////////////////

	////////////////////
	// db
	log.Default().Println("starting internal db service...")

	d, err := db.NewDB(chid, conf.DBSecret, conf.DBUser, conf.DBPassword, conf.DBName, conf.DBPort, conf.DBHost, conf.DBReaderHost)
	if err != nil {
		log.Fatal(err)
	}
	defer d.Close()
	////////////////////

	////////////////////
	// main error channel
	quitAck := make(chan error)
	defer close(quitAck)
	////////////////////

	////////////////////
	// pools
	pools := ws.NewConnectionPools()
	////////////////////

	////////////////////
	// webhook
	log.Default().Println("starting webhook service...")

	w := webhook.NewMessager(conf.DiscordURL, conf.ChainName, *notify)
	defer func() {
		if r := recover(); r != nil {
			// in case of a panic, notify the webhook messager with an error notification
			err := fmt.Errorf("recovered from panic: %v", r)
			log.Default().Println(err)
			w.NotifyError(ctx, err)
			// sentry.CaptureException(err)
		}
	}()

	w.Notify(ctx, "engine started")
	////////////////////

	////////////////////
	// push queue
	log.Default().Println("starting push queue service...")

	pu := queue.NewPushService()

	pushqueue, pushqerr := queue.NewService("push", 3, *useropqbf, ctx)
	defer pushqueue.Close()

	go func() {
		for err := range pushqerr {
			// TODO: handle errors coming from the queue
			w.NotifyError(ctx, err)
			log.Default().Println(err.Error())
		}
	}()

	go func() {
		quitAck <- pushqueue.Start(pu)
	}()
	////////////////////

	////////////////////
	// indexer
	if !*noindex {
		log.Default().Println("starting indexer service...")

		idx := indexer.NewIndexer(ctx, conf.RelayPrivateKey, chid, d, n, evm, pools)
		go func() {
			quitAck <- idx.Start()
		}()
	}
	////////////////////

	////////////////////
	// userop queue
	log.Default().Println("starting userop queue service...")

	op := queue.NewUserOpService(ctx, chid, d, n, evm, pushqueue, pools)

	useropq, qerr := queue.NewService("userop", 3, *useropqbf, ctx)
	defer useropq.Close()

	go func() {
		for err := range qerr {
			// TODO: handle errors coming from the queue
			w.NotifyError(ctx, err)
			log.Default().Println(err.Error())
		}
	}()

	go func() {
		quitAck <- useropq.Start(op)
	}()
	////////////////////

	////////////////////
	// api
	s := api.NewServer(chid, d, n, evm, useropq, pools)

	bu := bucket.NewBucket(conf.PinataBaseURL, conf.PinataAPIKey, conf.PinataAPISecret)

	wsr := s.CreateBaseRouter()
	wsr = s.AddMiddleware(wsr)
	wsr = s.AddRoutes(wsr, bu)

	go func() {
		quitAck <- s.Start(*port, wsr)
	}()

	log.Default().Println("listening on port: ", *port)
	////////////////////

	////////////////////
	// pubkey
	pubkey, err := common.PrivateKeyToPublicKey(conf.RelayPrivateKey)
	if err != nil {
		log.Fatal(err)
	}

	////////////////////

	////////////////////
	// nostr
	relay := khatru.NewRelay()

	relay.Info.Name = conf.RelayInfoName
	relay.Info.PubKey = pubkey
	relay.Info.Description = conf.RelayInfoDescription
	relay.Info.Icon = conf.RelayInfoIcon

	relay.StoreEvent = append(relay.StoreEvent, ndb.SaveEvent)
	relay.QueryEvents = append(relay.QueryEvents, ndb.QueryEvents)
	relay.CountEvents = append(relay.CountEvents, ndb.CountEvents)
	relay.DeleteEvent = append(relay.DeleteEvent, ndb.DeleteEvent)
	relay.ReplaceEvent = append(relay.ReplaceEvent, ndb.ReplaceEvent)

	go func() {
		log.Default().Println("relay running on port: 3334")
		quitAck <- http.ListenAndServe(":3334", relay)
	}()
	////////////////////

	for err := range quitAck {
		if err != nil {
			w.NotifyError(ctx, err)
			// sentry.CaptureException(err)
			log.Fatal(err)
		}
	}

	log.Default().Println("engine stopped")
}
