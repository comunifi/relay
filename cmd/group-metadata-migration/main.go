package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/comunifi/relay/internal/config"
	"github.com/comunifi/relay/pkg/common"
	"github.com/fiatjaf/eventstore/postgresql"
)

func main() {
	log.Default().Println("starting group metadata migration...")

	////////////////////
	// flags
	env := flag.String("env", ".env", "path to .env file")
	group := flag.String("group", "", "optional: migrate only a specific group ID")
	dryRun := flag.Bool("dry-run", false, "show what would be done without making changes")

	flag.Parse()
	////////////////////

	ctx := context.Background()

	log.Default().Println("env:", *env)
	if *dryRun {
		log.Default().Println("DRY RUN MODE - no changes will be made")
	}

	////////////////////
	// config
	conf, err := config.New(ctx, *env)
	if err != nil {
		log.Fatal(err)
	}
	////////////////////

	////////////////////
	// nostr-postgres
	log.Default().Println("connecting to database...")

	ndb := postgresql.PostgresBackend{
		DatabaseURL: fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
			conf.DBUser, conf.DBPassword, conf.DBHost, conf.DBPort, conf.DBName),
	}

	err = ndb.Init()
	if err != nil {
		log.Fatal(err)
	}
	defer ndb.Close()
	////////////////////

	////////////////////
	// pubkey
	pubkey, err := common.PrivateKeyToPublicKey(conf.RelayPrivateKey)
	if err != nil {
		log.Fatal(err)
	}

	log.Default().Println("relay pubkey:", pubkey)
	////////////////////

	////////////////////
	// run migration
	migrator := NewMigrator(&ndb, pubkey, conf.RelayPrivateKey, *dryRun)

	if *group != "" {
		// Migrate specific group
		log.Default().Printf("migrating single group: %s", *group)
		err = migrator.MigrateGroup(ctx, *group)
	} else {
		// Migrate all groups
		log.Default().Println("migrating all groups...")
		err = migrator.MigrateAllGroups(ctx)
	}

	if err != nil {
		log.Fatal(err)
	}
	////////////////////

	log.Default().Println("group metadata migration complete")
}

