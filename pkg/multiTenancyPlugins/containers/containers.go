package containers

import (
	"bytes"
	"encoding/json"
	log "github.com/Sirupsen/logrus"
	apitypes "github.com/docker/engine-api/types"
	"github.com/docker/swarm/cluster"
	"strings"
)

func createContainerMapping(returnCode int, body []byte, tenant string, name string) {
	if returnCode != 201 {
		return
	}
	var containerConfig apitypes.ContainerCreateResponse
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&containerConfig); err != nil {
		return
	}
	log.Debug("Mapping container: " + name)
	id := containerConfig.ID
	containers[id] = tenant // map fill ID
	if name != "" {
		containers[name+tenant] = tenant // map name
	}
	shortID := string(id[0:12])
	containers[shortID] = tenant // map short ID
}

func deleteContainerMapping(returnCode int, container *cluster.Container) {
	if returnCode >= 300 || returnCode < 200 {
		return
	}
	if container != nil {
		log.Debug("Deleting container mapping")
		id := container.Info.ID
		delete(containers, id)                 // delete ID mapping
		delete(containers, string(id[0:12]))   // delete short ID mapping
		for _, name := range container.Names { // delete name mapping
			delete(containers, strings.TrimPrefix(name, "/"))
		}
		return
	}
	log.Debug("Failed to delete container mapping")
}

func IsContainerExists(key string) bool {
	if _, ok := containers[key]; ok {
		return true
	}
	return false
}

func AddContainerMapping(container *cluster.Container, tenant string) {
	id := container.Info.ID
	log.Debug("Mapping container: " + id)
	containers[id] = tenant               // add ID mapping
	containers[string(id[0:12])] = tenant // add short ID mapping
	// Add name mapping
	for _, name := range container.Names {
		containers[strings.TrimPrefix(name, "/")] = tenant
	}
}
