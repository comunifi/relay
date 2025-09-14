package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/citizenwallet/engine/cmd/relay-tx-migration/logs"
	"github.com/citizenwallet/engine/internal/config"
	"github.com/citizenwallet/engine/internal/db"
	"github.com/citizenwallet/engine/internal/ethrequest"
	"github.com/citizenwallet/engine/pkg/common"
	"github.com/fiatjaf/eventstore/postgresql"
	"github.com/fiatjaf/khatru"
)

func main() {
	log.Default().Println("starting engine...")

	////////////////////
	// flags
	// port := flag.Int("port", 3001, "port to listen on")

	env := flag.String("env", ".env", "path to .env file")

	// polling := flag.Bool("polling", false, "enable polling")

	// noindex := flag.Bool("noindex", false, "disable indexing")

	// useropqbf := flag.Int("buffer", 1000, "userop queue buffer size (default: 1000)")

	// notify := flag.Bool("notify", false, "enable webhook notifications")

	flag.Parse()
	////////////////////

	ctx := context.Background()

	println("env", *env)

	////////////////////
	// config
	conf, err := config.New(ctx, *env)
	if err != nil {
		log.Fatal(err)
	}
	////////////////////
	////////////////////
	// evm
	rpcUrl := conf.RPCWSURL

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

	err = logs.MigrateLogs(ctx, evm, chid, conf.RelayPrivateKey, pubkey, d, &ndb)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("running on :3334")
	http.ListenAndServe(":3334", relay)

	////////////////////
	log.Default().Println("engine stopped")
}
