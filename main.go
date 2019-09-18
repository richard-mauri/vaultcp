package main

// See: https://godoc.org/github.com/hashicorp/vault/api
// func (c *Sys) Health() (*HealthResponse, error)

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"strconv"

	"github.com/hashicorp/vault/api"
)

var (
	kvRoot string = ""
	kvApi bool = false
)

func list(client *api.Client, path string) (keys []string, err error) {
	path = strings.TrimSuffix(path, "/")

	s, err := client.Logical().List(path)
	if err != nil {
		return keys, err
	}

	ikeys := s.Data["keys"].([]interface{})

	for _, ik := range ikeys {
		k := fmt.Sprint(ik)
		if strings.HasSuffix(k, "/") {
			k2 := strings.TrimSuffix(k, "/")
			p2 := fmt.Sprintf("%s/%s", path, k2)
			_, err = list(client, p2)
			if err != nil {
				return keys, err
			}
		} else {
			path2 := path
			if kvApi {
				path2 = strings.Replace(path, "metadata", "data", 1)
			}
			p2 := fmt.Sprintf("%s/%s", path2, k)
			output, err := read(client, p2)
			if err != nil {
				return keys, err
			}
			fmt.Printf("%s %s\n", p2, output)
		}
	}
	return keys, err
}

func read(client *api.Client, path string) (output string, err error) {
	s, err := client.Logical().Read(path)
	if err != nil {
		return output, err
	}

	if kvApi {
		ba, err := json.Marshal(s.Data["data"])
		if err != nil {
			return output, err
		}
		output = string(ba)
	} else {
		m, err := json.Marshal(s.Data)
		if err != nil {
			return output, err
		}
		output = string(m)
	}

	return output, err
}

func main() {
	addr := "http://127.0.0.1:8200"
	client, err := api.NewClient(&api.Config{
		Address: addr,
	})
	sys := client.Sys()
	// func (c *Sys) ListMounts() (map[string]*MountOutput, error)
	mounts, err := sys.ListMounts()
	if err != nil {
		log.Printf("Error listing mounts: %s", err)
		os.Exit(1)
	}

	for k, v := range(mounts) {
		if v.Type == "kv" {
			kvRoot = k;
			break
		}
	}

	// func (c *Sys) Health() (*HealthResponse, error)
	healthResponse, err := sys.Health()
	if err != nil {
		log.Printf("Error checking health: %s", err)
		os.Exit(1)
	}
	parts := strings.Split(healthResponse.Version, " ") // example: 0.9.5
	parts = strings.Split(parts[0], ".")
	majorVer, err := strconv.Atoi(parts[0])
	minorVer, err := strconv.Atoi(parts[1])
	kvApi = majorVer > 0 || minorVer >= 10;

	path := kvRoot
	if kvApi {
		path = fmt.Sprintf("%s/metadata", kvRoot)
	}

	_, err = list(client, path)
	if err != nil {
		log.Printf("Error listing secrets: %s", err)
		os.Exit(1)
	}
}
