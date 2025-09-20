package main

import (
	"encoding/hex"
	"fmt"
	"log"

	"github.com/comunifi/relay/pkg/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func main() {
	log.Default().Println("generating...")
	log.Default().Println(" ")

	k, err := common.GenerateKey()
	if err != nil {
		log.Fatal(err)
	}

	hexKey := hex.EncodeToString(k)

	// key address
	ecdsaKey, err := crypto.HexToECDSA(hexKey)
	if err != nil {
		log.Fatal(err)
	}

	keyAddress := crypto.PubkeyToAddress(ecdsaKey.PublicKey).Hex()

	println()
	println((fmt.Sprintf("key address: %s\n", keyAddress)))
	println((fmt.Sprintf("hex key: %s\n", hexKey)))
	println()
}
