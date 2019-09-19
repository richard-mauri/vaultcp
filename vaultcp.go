package main

// ./main -srcVaultToken bc124930-2f94-0dff-82d1-0f27e3def7f9 -srcVaultAddr https://internal-em-kr-V1PAp-JJ1FWB5N2S8N-1431743172.us-west-2.elb.amazonaws.com -dstVaultToken 7823288b-9c9c-59d7-957e-ef0f25601b50 -dstVaultAddr http://127.0.0.1:8200 -doCopy

// See: https://godoc.org/github.com/hashicorp/vault/api

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/hashicorp/vault/api"
)

var (
	kvRoot     string = ""
	kvApi      bool   = false
	srcClients []*api.Client
	dstClients []*api.Client
	listFile   *os.File

	// flags below
	numWorkers    *int
	doCopy        *bool
	doMirror      *bool
	srcVaultAddr  *string
	dstVaultAddr  *string
	srcVaultToken *string
	dstVaultToken *string
)

func list2(path string) (err error) {
	srcKV := map[string]interface{}{}
	err = list(srcClients[0], path, false, srcKV)
	if err != nil {
		return err
	}

	jobMaps := make([]map[string]interface{}, *numWorkers)
	for i := 0; i < *numWorkers; i++ {
		jobMaps[i] = make(map[string]interface{}, 1000)
	}

	count := 0
	for k, sv := range srcKV {
		count++
		jobMaps[count%*numWorkers][k] = sv
	}

	var wg sync.WaitGroup

	for w := 0; w < *numWorkers; w++ {
		wg.Add(1)
		go listWorker(w, jobMaps[w], &wg)
	}

	wg.Wait()

	return err //assert nil
}

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
		// TODO delete entries in dest when doMirror
	}

	// Divide the kv entries into numWorkers so we can update the dst Vault in parallel
	jobMaps := make([]map[string]interface{}, *numWorkers)
	for i := 0; i < *numWorkers; i++ {
		jobMaps[i] = make(map[string]interface{}, 1000)
	}

	count := 0
	for k, sv := range srcKV {
		dv := dstKV[k]
		if dv == "" || dv == nil {
			log.Printf("Copy (for create) src to dst vault: %s value: %s\n", k, sv)
			count++
			jobMaps[count%*numWorkers][k] = sv
		} else if sv != dv {
			log.Printf("Copy (for update?) src to dst vault: %s value: %s\n", k, sv)
			count++
			jobMaps[count%*numWorkers][k] = sv
		} else {
			log.Printf("The src and dst Vaults have a matching kv entry for key %s\n", k)
		}
	}

	var wg sync.WaitGroup

	for w := 0; w < *numWorkers; w++ {
		wg.Add(1)
		go writeWorker(w, jobMaps[w], &wg)
	}

	wg.Wait()

	return err //assert nil
}

func listWorker(id int, job map[string]interface{}, wg *sync.WaitGroup) {
	log.Println("list worker", id, "starting job of ", len(job), " keys")
	for k, v := range job {
		// Assert v == nil; lazy read to help parallelization
		log.Printf("list worker %d reading %s\n", id, k)
		var err error
		v, err = readRaw(srcClients[id], k)
		if err != nil {
			log.Printf("Error from readRaw: %s\n", err)
		}

		// print just the data element as we expect the metadata to be different, which would make determining diffs hard
		vComplete := v.(map[string]interface{})
		if kvApi {
			vData := vComplete["data"]
			v2, err := marshalData(vData.(map[string]interface{}))
			if err != nil {
				log.Printf("Error from marshalData: %s\n", err)
			}
			line := fmt.Sprintf("%s %s\n", k, v2)
			_, err = listFile.WriteString(line)
			if err != nil {
				log.Printf("Error from listFile.WriteString(%s): %s\n", line, err)
			}
		} else {
			v2, err := marshalData(vComplete)
			if err != nil {
				log.Printf("Error from marshalData: %s\n", err)
			}
			line := fmt.Sprintf("%s %s\n", k, v2)
			_, err = listFile.WriteString(line)
			if err != nil {
				log.Printf("Error from listFile.WriteString(%s): %s\n", line, err)
			}
		}
	}
	log.Println("list worker", id, "finished job of", len(job), " keys")
	wg.Done()
}

func writeWorker(id int, job map[string]interface{}, wg *sync.WaitGroup) {
	fmt.Println("write worker", id, "starting write job of ", len(job), " keys")
	for k, v := range job {
		// Assert v == nil; lazy read to help parallelization
		log.Printf("write worker %d reading %s\n", id, k)
		var err error
		v, err = readRaw(srcClients[id], k)
		if err != nil {
			log.Printf("Error from readRaw: %s\n", err)
		}

		log.Printf("write worker %d writing %s => %v\n", id, k, v)
		_, err = dstClients[id].Logical().Write(k, v.(map[string]interface{}))
		if err != nil {
			log.Printf("Error from Vault write: %s\n", err)
		}
	}
	fmt.Println("write worker", id, "finished write job of", len(job), " keys")
	wg.Done()
}

func list(client *api.Client, path string, outputAndRead bool, kv map[string]interface{}) (err error) {
	path = strings.TrimSuffix(path, "/")

	s, err := client.Logical().List(path)
	if err != nil {
		return err
	}

	if s == nil {
		return // no entries
	}

	ikeys := s.Data["keys"].([]interface{})

	for _, ik := range ikeys {
		k := fmt.Sprint(ik)
		if strings.HasSuffix(k, "/") {
			k2 := strings.TrimSuffix(k, "/")
			p2 := fmt.Sprintf("%s/%s", path, k2)
			err = list(client, p2, outputAndRead, kv)
			if err != nil {
				return err
			}
		} else {
			path2 := path
			if kvApi {
				path2 = strings.Replace(path, "metadata", "data", 1)
			}
			p2 := fmt.Sprintf("%s/%s", path2, k)
			kv[p2] = nil // Intent is to lazy read
			if outputAndRead {
				value, err := readRaw(client, p2)
				if err != nil {
					return err
				}
				kv[p2] = value
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

func fetchVersionInfo(client *api.Client) (kvApiLocal bool, kvRoot string, err error) {
	sys := client.Sys()
	mounts, err := sys.ListMounts()
	if err != nil {
		return kvApiLocal, kvRoot, err
	}

	for k, v := range mounts {
		if v.Type == "kv" {
			kvRoot = k
			break
		}
	}

	healthResponse, err := sys.Health()
	if err != nil {
		return kvApiLocal, kvRoot, err
	}
	parts := strings.Split(healthResponse.Version, " ") // example: 0.9.5
	parts = strings.Split(parts[0], ".")
	majorVer, err := strconv.Atoi(parts[0])
	minorVer, err := strconv.Atoi(parts[1])
	kvApiLocal = majorVer > 0 || minorVer >= 10

	return kvApiLocal, kvRoot, err
}

func prep() {
	numWorkers = flag.Int("numWorkers", 10, "Number of workers to enable parallel execution")
	doCopy = flag.Bool("doCopy", false, "Copy the secrets from the source to destination Vault (default: false)")
	doMirror = flag.Bool("doMirror", false, "Like doCopy but destination Vault entries not in the source Vault will be deleted (default: false)")
	srcVaultAddr = flag.String("srcVaultAddr", "http://127.0.0.1:8200", "Source Vault address (required)")
	srcVaultToken = flag.String("srcVaultToken", "", "Source Vault token (required)")
	dstVaultAddr = flag.String("dstVaultAddr", "", "Destination Vault address (required for doCopy and doMirror)")
	dstVaultToken = flag.String("dstVaultToken", "", "Destination Vault token (required for doCopy and doMirror)")
	flag.Parse()

	if *numWorkers < 1 {
		log.Printf("Error: Illegal value %d for numWorkers; it must be > 0\n", *numWorkers)
		os.Exit(1)
	}

	var err error
	var srcClient *api.Client
	var dstClient *api.Client
	srcClients = make([]*api.Client, *numWorkers)
	dstClients = make([]*api.Client, *numWorkers)
	for i := 0; i < *numWorkers; i++ {
		srcClient, err = api.NewClient(&api.Config{
			Address: *srcVaultAddr,
		})
		if err != nil {
			log.Printf("Error from vault NewClient : %s\n", err)
			os.Exit(1)
		}
		srcClient.SetToken(*srcVaultToken)
		srcClients[i] = srcClient

		if *doCopy || *doMirror {
			dstClient, err = api.NewClient(&api.Config{
				Address: *dstVaultAddr,
			})
			if err != nil {
				log.Printf("Error from vault NewClient : %s\n", err)
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

	if *doCopy || *doMirror {
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
	}
	kvApi = srcKvApi
	kvRoot = srcKvRoot
}

func main() {
	prep()

	var path string
	if kvApi {
		if strings.HasSuffix(kvRoot, "/") {
			path = fmt.Sprintf("%smetadata", kvRoot)
		} else {
			path = fmt.Sprintf("%s/metadata", kvRoot)
		}
	} else {
		path = kvRoot
	}

	if *doCopy || *doMirror {
		err := copy(path)
		if err != nil {
			log.Printf("Error copying secrets: %s", err)
			os.Exit(1)
		}
	} else {
		var err error
		listFile, err = os.Create("/tmp/vaultcp.out")
		if err != nil {
			log.Printf("Error creating list output file: %s", err)
			os.Exit(1)
		}
		defer listFile.Close()

		err = list2(path)
		if err != nil {
			log.Printf("Error listing secrets: %s", err)
			os.Exit(1)
		}
	}
}
