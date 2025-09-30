package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/comunifi/relay/cmd/relay-tx-migration/logs"
	"github.com/comunifi/relay/cmd/relay-tx-migration/logs/logdb"
	"github.com/comunifi/relay/internal/config"
	"github.com/comunifi/relay/internal/ethrequest"
	nost "github.com/comunifi/relay/internal/nostr"
	"github.com/comunifi/relay/pkg/common"
	"github.com/fiatjaf/eventstore/postgresql"
	"github.com/fiatjaf/khatru"
)

func main() {
	log.Default().Println("starting relay...")

	////////////////////
	// flags
	group := flag.String("group", "", "group to use for filtering logs")

	env := flag.String("env", ".env", "path to .env file")

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

	d, err := logdb.NewDB(chid, conf.DBSecret, conf.DBUser, conf.DBPassword, conf.DBName, conf.DBPort, conf.DBHost, conf.DBReaderHost)
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

	////////////////////
	////////////////////
	// nostr-service
	n := nost.NewNostr(conf.RelayPrivateKey, &ndb, relay, conf.RelayUrl)

	////////////////////
	err = logs.MigrateLogs(ctx, evm, chid, group, conf.RelayPrivateKey, pubkey, d, n)
	if err != nil {
		log.Fatal(err)
	}

	log.Default().Println("data migration complete")
}
