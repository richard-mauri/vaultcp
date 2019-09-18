package main

// See: https://godoc.org/github.com/hashicorp/vault/api

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/hashicorp/vault/api"
)

func list(client *api.Client, path string) (keys []string, err error) {
	s, err := client.Logical().List(path)
	if err != nil {
		return keys, err
	}

	ikeys := s.Data["keys"].([]interface{})

	keys = make([]string, len(ikeys))
	for i, v := range ikeys {
		keys[i] = fmt.Sprint(v)
	}
	return keys, err
}

func read(client *api.Client, path string) (output string, err error) {
	s, err := client.Logical().Read(path)
	if err != nil {
		return output, err
	}

	ba, err := json.Marshal(s.Data["data"])
	if err == nil {
		output = string(ba)
	}
	return output, err
}

func main() {
	addr := "http://127.0.0.1:8200"
	client, err := api.NewClient(&api.Config{
		Address: addr,
	})
	keys, err := list(client, "secret/metadata")
	if err != nil {
		log.Printf("Error listing secret: %s", err)
		os.Exit(1)
	}
	for i, k := range keys {
		log.Printf("key %d = %s\n", i, k)
	}

	output, err := read(client, "secret/data/s1")
	if err != nil {
		log.Printf("Error reading secret: %s", err)
		os.Exit(1)
	}
	log.Printf("read output = %s\n", output)
}
