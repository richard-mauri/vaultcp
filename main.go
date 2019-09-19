package main

// ./main -srcVaultToken bc124930-2f94-0dff-82d1-0f27e3def7f9 -srcVaultAddr https://internal-em-kr-V1PAp-JJ1FWB5N2S8N-1431743172.us-west-2.elb.amazonaws.com -dstVaultToken 7823288b-9c9c-59d7-957e-ef0f25601b50 -dstVaultAddr http://127.0.0.1:8200 -doCopy

// See: https://godoc.org/github.com/hashicorp/vault/api

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"strconv"
	"sync"

	"github.com/hashicorp/vault/api"
)

const (
	NumWorkers = 10
)

var (
	kvRoot string = ""
	kvApi bool = false
	srcClients []*api.Client
	dstClients []*api.Client

	// flags below
	doCopy *bool 
	srcVaultAddr *string
	dstVaultAddr *string
	srcVaultToken *string
	dstVaultToken *string
)

func copy(path string) (err error) {
	srcKV := map[string]interface{}{}
	err = list(srcClients[0], path, false, srcKV)
	if err != nil {
		return err
	}

	dstKV := map[string]interface{}{}
	err = list(dstClients[0], path, false, dstKV)
	if err != nil {
		return err
	}

	numdstkeys := len(dstKV)
	if numdstkeys > 0 {
		log.Printf("Warning: The destination Vault already has %d keys\n", numdstkeys)
		// TODO: determine if these are in the source Vault and if so optionally overwrite them if the data is different
	}

	numsrckeys := len(srcKV)
	log.Printf("Info: The source Vault has %d keys\n", numsrckeys)

	if numsrckeys == 0 {
		log.Printf("Warning: The source Vault has no keys\n")
	}

	// Divide the kv entries into NumWorkers so we can update the dst Vault in parallel
	jobMaps := make([]map[string]interface{}, NumWorkers)
	for i := 0; i < NumWorkers; i++ {
		jobMaps[i] = make(map[string]interface{}, 1000)
	}

	count := 0
	for k,sv := range(srcKV) {
		dv := dstKV[k]
		// log.Printf("key = %s src value = %v dst value = %v\n", k, sv, dv)
		if dv == "" || dv == nil {
			log.Printf("Copy (for create) src to dst vault: %s value: %s\n", k, sv)
			count++
			jobMaps[count % NumWorkers][k] = sv
		} else if sv != dv {
			log.Printf("Copy (for update?) src to dst vault: %s value: %s\n", k, sv)
			count++
			jobMaps[count % NumWorkers][k] = sv
		} else {
			log.Printf("The src and dst Vaults have a matching kv entry for key %s\n", k)
		}
	}

	var wg sync.WaitGroup

	for w := 0; w < NumWorkers; w++ {
		wg.Add(1)
        	go worker(w, jobMaps[w], &wg)
	}

	wg.Wait()

	return err //assert nil
}

func worker(id int, job map[string]interface{}, wg *sync.WaitGroup) {
	fmt.Println("worker", id, "starting write job of ", len(job) , " keys")
	for k,v := range job {
		// func (c *Logical) Write(path string, data map[string]interface{}) (*Secret, error)
		log.Printf("worker %d writing %s => %v\n", id, k, v)
		data := v.(map[string]interface {})
		_, err := dstClients[id].Logical().Write(k, data)
		if err != nil {
			log.Printf("Error: %s\n", err)
		}
	}
	fmt.Println("worker", id, "finished write job of", len(job), " keys")
	wg.Done()
}

func list(client *api.Client, path string, output bool, kv map[string]interface{}) (err error) {
	path = strings.TrimSuffix(path, "/")

	log.Printf("Listing path %s with client %+v\n", path, client)
	s, err := client.Logical().List(path)
	if err != nil {
		return err
	}

	ikeys := s.Data["keys"].([]interface{})

	for _, ik := range ikeys {
		k := fmt.Sprint(ik)
		if strings.HasSuffix(k, "/") {
			k2 := strings.TrimSuffix(k, "/")
			p2 := fmt.Sprintf("%s/%s", path, k2)
			err = list(client, p2, output, kv)
			if err != nil {
				return err
			}
		} else {
			path2 := path
			if kvApi {
				path2 = strings.Replace(path, "metadata", "data", 1)
			}
			p2 := fmt.Sprintf("%s/%s", path2, k)
			value, err := readRaw(client, p2)
			if err != nil {
				return err
			}
			kv[p2] = value
			if output {
				v, err := marshalData(value)
				if err != nil {
					return err
				}
				fmt.Printf("%s %v\n", p2, v)
			}
		}
	}
	return err
}

func marshalData(data map[string]interface{}) (value string, err error) {
	ba, err := json.Marshal(data)
	if err != nil {
		return value, err
	}
	value = string(ba)
	return value, err
}

func read(client *api.Client, path string) (value string, err error) {
	data, err := readRaw(client, path)
	if err != nil {
		return value, err
	}
	return marshalData(data)
}

func readRaw(client *api.Client, path string) (value map[string]interface{}, err error) {
	s, err := client.Logical().Read(path)
	if err != nil {
		return value, err
	}

	if kvApi {
		// value = s.Data["data"] // REVISIT
		value = s.Data
	} else {
		value = s.Data
	}

	return value, err
}

func fetchVersionInfo(client *api.Client) (kvApi bool, kvRoot string, err error) {
	sys := client.Sys()
	mounts, err := sys.ListMounts()
	if err != nil {
		return kvApi, kvRoot, err
	}

	for k, v := range(mounts) {
		if v.Type == "kv" {
			kvRoot = k;
			break
		}
	}

	healthResponse, err := sys.Health()
	if err != nil {
		return kvApi, kvRoot, err
	}
	parts := strings.Split(healthResponse.Version, " ") // example: 0.9.5
	parts = strings.Split(parts[0], ".")
	majorVer, err := strconv.Atoi(parts[0])
	minorVer, err := strconv.Atoi(parts[1])
	kvApi = majorVer > 0 || minorVer >= 10;

	return kvApi, kvRoot, err
}

func main() {
	doCopy = flag.Bool("doCopy", false, "copy the screts from source to destination Vaults (default: false)")
	srcVaultAddr = flag.String("srcVaultAddr", "http://127.0.0.1:8200", "Source Vault address (required)")
	srcVaultToken = flag.String("srcVaultToken", "", "Source Vault token (required)")
	dstVaultAddr = flag.String("dstVaultAddr", "", "Destination Vault address (required for doCopy)")
	dstVaultToken = flag.String("dstVaultToken", "", "Destination Vault token (required for doCopy)")
	flag.Parse()

	var err error
	var srcClient *api.Client
	var dstClient *api.Client
	srcClients = make([]*api.Client, NumWorkers)
	dstClients = make([]*api.Client, NumWorkers)
	for i := 0; i < NumWorkers; i++ {
		srcClient, err = api.NewClient(&api.Config{
			Address: *srcVaultAddr,
		})
		if err != nil {
			log.Printf("Error: %s\n", err)
			os.Exit(1)
		}
		srcClient.SetToken(*srcVaultToken)
		srcClients[i] = srcClient

		if *doCopy {
			dstClient, err = api.NewClient(&api.Config{
				Address: *dstVaultAddr,
			})
			if err != nil {
				log.Printf("Error: %s\n", err)
				os.Exit(1)
			}
			dstClient.SetToken(*dstVaultToken)
			dstClients[i] = dstClient
		}
	}

	srcKvApi, srcKvRoot, err := fetchVersionInfo(srcClients[0])
	if err != nil {
		log.Printf("Error fetching version info: %s", err)
		os.Exit(1)
	}

	if *doCopy {
		if *dstVaultAddr == "" {
			log.Printf("Unspecified dstVaultAddr\n")
			os.Exit(1)
		}

		if *dstVaultToken == "" {
			log.Printf("Unspecified dstVaultToken\n")
			os.Exit(1)
		}

		dstKvApi, dstKvRoot, err := fetchVersionInfo(dstClients[0])
		if err != nil {
			log.Printf("Error fetching version info: %s", err)
			os.Exit(1)
		}

		if dstKvApi != srcKvApi {
			log.Printf("The Vault kv api is different betwen the source and destination Vaults\n")
			os.Exit(1)
		}
		if dstKvRoot != srcKvRoot {
			log.Printf("The Vault kv root is different betwen the source and destination Vaults\n")
			os.Exit(1)
		}
		kvApi = srcKvApi
		kvRoot = srcKvRoot

		path := srcKvRoot 
		if srcKvApi {
			path = fmt.Sprintf("%s/metadata", srcKvRoot) // same path for dst vault
		}

		err = copy(path)
		if err != nil {
			log.Printf("Error copying secrets: %s", err)
			os.Exit(1)
		}
	} else {
		kvApi = srcKvApi
		kvRoot = srcKvRoot
		path := srcKvRoot 
		if srcKvApi {
			path = fmt.Sprintf("%s/metadata", srcKvRoot)
		}

		srcKV := map[string]interface{}{}
		err = list(srcClients[0], path, true, srcKV)
		if err != nil {
			log.Printf("Error listing secrets: %s", err)
			os.Exit(1)
		}
	}
}
