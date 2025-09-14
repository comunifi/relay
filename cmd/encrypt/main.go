package main

import (
	"flag"
	"log"

	"github.com/citizenapp2/relay/pkg/common"
)

func main() {
	log.Default().Println("generating...")
	log.Default().Println(" ")

	s := flag.String("s", "", "the key to be used to encrypt the value")

	v := flag.String("v", "", "the value to be encrypted")

	flag.Parse()

	k, err := common.Encrypt(*v, *s)
	if err != nil {
		log.Fatal(err)
	}

	log.Default().Printf("key: %s\n", k)
}
