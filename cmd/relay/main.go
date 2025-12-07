package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/comunifi/relay/internal/blossom"
	"github.com/comunifi/relay/internal/config"
	"github.com/comunifi/relay/internal/ethrequest"
	"github.com/comunifi/relay/pkg/common"
	"github.com/fiatjaf/eventstore/postgresql"
	"github.com/fiatjaf/khatru"
)

func main() {
	log.Default().Println("starting relay...")

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
	// db
	log.Default().Println("starting internal db service...")

	db := postgresql.PostgresBackend{
		DatabaseURL: fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", conf.DBUser, conf.DBPassword, conf.DBHost, conf.DBPort, conf.DBName),
	}

	err = db.Init()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
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

	relay.StoreEvent = append(relay.StoreEvent, db.SaveEvent)
	relay.QueryEvents = append(relay.QueryEvents, db.QueryEvents)
	relay.CountEvents = append(relay.CountEvents, db.CountEvents)
	relay.DeleteEvent = append(relay.DeleteEvent, db.DeleteEvent)
	relay.ReplaceEvent = append(relay.ReplaceEvent, db.ReplaceEvent)

	////////////////////
	// blossom (media storage)
	if conf.AWSS3BucketName != "" && conf.AWSAccessKeyID != "" && conf.AWSSecretAccessKey != "" {
		log.Default().Println("starting blossom media service...")

		// Create a separate database connection for blob metadata
		// Note: Using same DB for simplicity, but could use a separate DB in production
		blobDB := postgresql.PostgresBackend{
			DatabaseURL: fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", conf.DBUser, conf.DBPassword, conf.DBHost, conf.DBPort, conf.DBName),
		}
		if err := blobDB.Init(); err != nil {
			log.Fatal("failed to initialize blob metadata database:", err)
		}
		defer blobDB.Close()

		blossomCfg := &blossom.BlossomConfig{
			ServiceURL:      conf.RelayUrl,
			AWSAccessKeyID:  conf.AWSAccessKeyID,
			AWSSecretKey:    conf.AWSSecretAccessKey,
			AWSRegion:       conf.AWSDefaultRegion,
			AWSEndpointURL:  conf.AWSEndpointUrl,
			AWSS3BucketName: conf.AWSS3BucketName,
		}

		// Pass blobDB for blob metadata, and db for querying group membership events
		_, err := blossom.NewBlossomService(ctx, relay, &blobDB, &db, blossomCfg)
		if err != nil {
			log.Fatal("failed to initialize blossom service:", err)
		}

		log.Default().Println("blossom media service initialized with 50MB upload limit")
	} else {
		log.Default().Println("blossom media service disabled (S3 credentials not configured)")
	}
	////////////////////

	fmt.Println("running on :3334")
	http.ListenAndServe(":3334", relay)

	////////////////////
	log.Default().Println("engine stopped")
}
