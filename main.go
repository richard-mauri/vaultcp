package main

// See: https://godoc.org/github.com/hashicorp/vault/api

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/hashicorp/vault/api"
)

func list(client *api.Client, path string) (keys []string, err error) {
	s, err := client.Logical().List(path)
	if err != nil {
		return keys, err
	}

	ikeys := s.Data["keys"].([]interface{})

	/*
		keys = make([]string, len(ikeys))
		for i, v := range ikeys {
			keys[i] = fmt.Sprint(v)
		}
	*/
	for _, ik := range ikeys {
		k := fmt.Sprint(ik)
		// for _, k := range keys {
		if strings.HasSuffix(k, "/") {
			k2 := strings.TrimSuffix(k, "/")
			p2 := fmt.Sprintf("%s/%s", path, k2)
			_, err = list(client, p2)
			if err != nil {
				return keys, err
			}
		} else {
			path2 := strings.Replace(path, "metadata", "data", 1)
			p2 := fmt.Sprintf("%s/%s", path2, k)
			output, err := read(client, p2)
			if err != nil {
				return keys, err
			}
			log.Printf("%s %s\n", p2, output)
		}
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
	_, err = list(client, "secret/metadata")
	if err != nil {
		log.Printf("Error listing secret: %s", err)
		os.Exit(1)
	}
}
