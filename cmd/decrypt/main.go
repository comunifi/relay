package main

import (
	"flag"
	"log"

	"github.com/comunifi/relay/pkg/common"
)

func main() {
	log.Default().Println("decrypting...")
	log.Default().Println(" ")

	s := flag.String("s", "", "the key to be used to decrypt the value")

	v := flag.String("v", "", "the value to be decrypted")

	flag.Parse()

	k, err := common.Decrypt(*v, *s)
	if err != nil {
		log.Fatal(err)
	}

	log.Default().Printf("original value: %s\n", k)

}
