package utils

import (
	"bytes"
	"encoding/json"
	"errors"
	log "github.com/Sirupsen/logrus"
	apitypes "github.com/docker/engine-api/types"
	"github.com/docker/swarm/cluster"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/headers"
	"strings"
)

var FullIdToTenant map[string]string
var ContainerFullIds map[string]*cluster.Container
var ContainerShortIds map[string]*cluster.Container
var ContainerNames map[string]*cluster.Container

// Returns full container ID for container accociated with containerReference.
func GetContainerID(cluster cluster.Cluster, tenantId string, containerReference string) (string, error) {
	if tenant := FullIdToTenant[containerReference]; tenant == tenantId {
		return containerReference, nil
	}
	if container := GetContainer(containerReference, tenantId, cluster); container != nil {
		return container.Info.ID, nil
	}
	return "", errors.New("Not Authorized or no such resource!")
}

// Returns container accociated with containerReference.
func GetContainer(containerReference string, tenantID string, cluster cluster.Cluster) *cluster.Container {
	// check for long id
	if container := ContainerFullIds[containerReference]; container != nil {
		return container
	}
	// check for name
	if container := ContainerNames[containerReference+tenantID]; container != nil {
		return container
	}
	if container := ContainerNames[containerReference]; container != nil {
		return container
	}
	// check for short id
	if container := ContainerShortIds[containerReference]; container != nil {
		return container
	}
	log.Debug("Container not mapped.Search the cluster.")
	// Find on the cluster and map container.
	if container := findOnCluster(cluster, containerReference, tenantID); container != nil {
		FullIdToTenant[container.Info.ID] = container.Labels[headers.TenancyLabel]
		doMapping(container)
		return container
	}
	return nil
}


func InitialMapping(cluster cluster.Cluster) {
	log.Debug("Initial containers mapping.")
	for _, container := range cluster.Containers() {
		FullIdToTenant[container.Info.ID] = container.Labels[headers.TenancyLabel]
		doMapping(container)
	}
}


func CreateContainerMapping(returnCode int, body []byte, tenantID string, cluster cluster.Cluster) {
	if returnCode != 201 {
		return
	}
	// Get container ID from responce body
	id := getIdFromBody(body)
	if id == "" {
		log.Debug("Failed to map container.")
		return
	}
	// Map container full ID
	FullIdToTenant[id] = tenantID
	// Map container
	go mapContainer(id, cluster)
}


func DeleteContainerMapping(returnCode int, containerID string, containerNames []string, tenantID string, originalName string) {
	if returnCode >= 300 || returnCode < 200 {
		return
	}
	delete(ContainerFullIds, containerID) // delete ID mapping
	delete(FullIdToTenant, containerID)
	delete(ContainerShortIds, string(containerID[0:12])) // delete short ID mapping
	// delete names mapping
	delete(ContainerNames, originalName + tenantID)
	for _, name := range containerNames {
		delete(ContainerNames, strings.TrimPrefix(name, "/"))
	}
}


func IsOwnedByTenant(tenant string, containerID string) bool {
	if container := ContainerFullIds[containerID]; container != nil {
		return container.Labels[headers.TenancyLabel] == tenant
	}
	if tenantID := FullIdToTenant[containerID]; tenantID != "" {
		return tenantID == tenant
	}
	return false
}


func mapContainer(containerID string, cluster cluster.Cluster) {
	// Find container on the cluster
	container := getContainerFromCluster(containerID, cluster)
	if container == nil {
		log.Debug("Failed to map container.")
		return
	}
	doMapping(container)
}


func doMapping(container *cluster.Container) {
	ContainerFullIds[container.Info.ID] = container
	ContainerNames[container.Labels[headers.OriginalNameLabel]+container.Labels[headers.TenancyLabel]] = container
	for _, name := range container.Names {
		ContainerNames[strings.TrimPrefix(name, "/")] = container
	}
	ContainerShortIds[container.Info.ID[0:12]] = container
}


func getIdFromBody(body []byte) string {
	var containerConfig apitypes.ContainerCreateResponse
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&containerConfig); err != nil {
		log.Debug(err)
		return ""
	}
	return containerConfig.ID
}


func getContainerFromCluster(containerID string, cluster cluster.Cluster) *cluster.Container {
	for _, container := range cluster.Containers() {
		if container.Info.ID == containerID {
			return container
		}
	}
	return nil
}


func findOnCluster(cluster cluster.Cluster, containerReference string, tenantID string) *cluster.Container {
	for _, container := range cluster.Containers() {
		if container.Labels[headers.TenancyLabel] != tenantID {
			continue
		}
		if container.Info.ID == containerReference {
			//Match by Full Id
			return container
		}
		if containerReference == container.Labels[headers.OriginalNameLabel] {
			//Match by Name
			return container
		}
		for _, name := range container.Names {
			if containerReference == strings.TrimPrefix(name, "/") {
				//Match by Name
				return container
			}
		}
		if strings.HasPrefix(container.Info.ID, containerReference) {
			//Match by short ID
			return container
		}
	}
	return nil
}
