package namescoping

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm/cluster"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/headers"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/pluginAPI"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/utils"
	"github.com/gorilla/mux"
	"github.com/samalba/dockerclient"
	"io/ioutil"
	"net/http"
	"strings"
)

const NOTAUTHORIZED_ERROR = "No such container or the user is not authorized for this container: %s."
const CONTAINER_NOT_OWNED_INFO = "container not owned by current tenant info."
const CONTAINER_REFERENCE_NOT_FOR_CONTAINER_INFO = "container reference does not match this containter info."

//AuthenticationImpl - implementation of plugin API
type DefaultNameScopingImpl struct {
	nextHandler pluginAPI.Handler
}

func NewNameScoping(handler pluginAPI.Handler) pluginAPI.PluginAPI {
	nameScoping := &DefaultNameScopingImpl{
		nextHandler: handler,
	}
	return nameScoping
}

func getContainerID(cluster cluster.Cluster, r *http.Request, containerName string) (string, error) {
	tenantId := r.Header.Get(headers.AuthZTenantIdHeaderName)
	for _, container := range cluster.Containers() {
		if container.Info.ID == containerName {
			//Match by Full Id
			return container.Info.ID, nil
		} else {
			for _, name := range container.Names {
				if (containerName == strings.TrimPrefix(name, "/") || containerName == container.Labels[headers.OriginalNameLabel]) && container.Labels[headers.TenancyLabel] == tenantId {
					//Match by Name
					return container.Info.ID, nil
				}
			}
		}
		if strings.HasPrefix(container.Info.ID, containerName) {
			//Match by short ID
			return container.Info.ID, nil
		}
	}
	return containerName, errors.New("No such container")
}

//Handle authentication on request and call next plugin handler.
func (nameScoping *DefaultNameScopingImpl) Handle(command utils.CommandEnum, cluster cluster.Cluster, w http.ResponseWriter, r *http.Request, swarmHandler http.Handler) error {
	log.Debug("Plugin nameScoping Got command: " + command)
	switch command {
	case utils.CONTAINER_CREATE:
		var newQuery string
		var buf bytes.Buffer
		var containerConfig dockerclient.ContainerConfig
		var reqBody []byte
		defer r.Body.Close()
		if reqBody, _ = ioutil.ReadAll(r.Body); len(reqBody) > 0 {
			if err := json.NewDecoder(bytes.NewReader(reqBody)).Decode(&containerConfig); err != nil {
				log.Error(err)
				return err
			}
		}
		if "" != r.URL.Query().Get("name") {
			log.Debug("Postfixing name with tenantID...")
			newQuery = strings.Replace(r.RequestURI, r.URL.Query().Get("name"), r.URL.Query().Get("name")+r.Header.Get(headers.AuthZTenantIdHeaderName), 1)
			containerConfig.Labels[headers.OriginalNameLabel] = r.URL.Query().Get("name")
		}
		containerConfig.HostConfig.NetworkMode = getNetworkID(cluster, r, containerConfig.HostConfig.NetworkMode)
		if err := CheckContainerReferences(cluster, r.Header.Get(headers.AuthZTenantIdHeaderName), &containerConfig); err != nil {
			log.Error(err)
			return err
		}
		if len(reqBody) > 0 {
			if err := json.NewEncoder(&buf).Encode(containerConfig); err != nil {
				log.Error(err)
				return err
			}
			r, _ = utils.ModifyRequest(r, bytes.NewReader(buf.Bytes()), newQuery, "")
		}

		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)

	case utils.CONTAINER_JSON, utils.CONTAINER_START, utils.CONTAINER_STOP, utils.CONTAINER_RESTART, utils.CONTAINER_DELETE, utils.CONTAINER_WAIT, utils.CONTAINER_ARCHIVE, utils.CONTAINER_KILL, utils.CONTAINER_PAUSE, utils.CONTAINER_UNPAUSE, utils.CONTAINER_UPDATE, utils.CONTAINER_COPY, utils.CONTAINER_CHANGES, utils.CONTAINER_ATTACH, utils.CONTAINER_LOGS, utils.CONTAINER_TOP, utils.CONTAINER_STATS, utils.CONTAINER_EXEC:
		containerName := mux.Vars(r)["name"]
		conatinerID, err := getContainerID(cluster, r, containerName)
		if err != nil {
			return err
		}
		mux.Vars(r)["name"] = conatinerID
		r.URL.Path = strings.Replace(r.URL.Path, containerName, conatinerID, 1)
		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)

	case utils.NETWORK_CONNECT, utils.NETWORK_DISCONNECT:
		if err := ConnectDisconnect(cluster, r); err != nil {
			return err
		}
		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)

	case utils.NETWORK_CREATE:
		if err := CreateNetwork(cluster, r); err != nil {
			return err
		}
		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)

	case utils.NETWORK_INSPECT, utils.NETWORK_DELETE:
		DeleteInspect(cluster, r)
		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)

	case utils.PS, utils.JSON, utils.NETWORKS_LIST, utils.INFO, utils.EVENTS, utils.IMAGES_JSON, utils.EXEC_START, utils.EXEC_RESIZE, utils.EXEC_JSON, utils.IMAGE_PULL, utils.IMAGE_SEARCH, utils.IMAGE_HISTORY:
		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)

	default:

	}
	return nil
}

func CheckContainerReferences(cluster cluster.Cluster, tenantId string, containerConfig *dockerclient.ContainerConfig) error {
	log.Debugf("CheckContainerReferences containerConfig: %+v", containerConfig)
	// create arrays of container references to pass to getIDsFromContainerReferences

	// create links array
	linkContainerRefs := make([]string, len(containerConfig.HostConfig.Links))
	for i, link := range containerConfig.HostConfig.Links {
		// link in form [container reference]:[alias]
		// : and alias are optional
		containerRef_alias := strings.SplitN(link, ":", 2)
		linkContainerRefs[i] = strings.TrimSpace(containerRef_alias[0])
	}

	var err error
	var containerReferenceToIdMap map[string]string
	containerRefs := make([]string, 0)
	containerRefs = append(containerRefs, containerConfig.HostConfig.VolumesFrom...)
	containerRefs = append(containerRefs, linkContainerRefs...)
	if containerReferenceToIdMap, err = getIDsFromContainerReferences(cluster, tenantId, containerRefs); err != nil {
		return err
	}
	// ******************update containerConfig******************
	// update VolumesFrom
	for i, k := range containerConfig.HostConfig.VolumesFrom {
		containerConfig.HostConfig.VolumesFrom[i] = containerReferenceToIdMap[k]
	}

	// update links
	// We want to create an array of links with no duplicates.  Ideally, to do this we would use a set however
	// go does not support a native set structure.  Rather we use a map, named linkSet, to accumulated non duplicate links.
	// Only the keys are important in linkSet;  the values are meaningless.
	// Once linkSet is created we generate the links array from linkSet.
	// We want to generate a super set of links with no duplicates.  However go does not support a native set,
	// do we use map that points to an empty structure to simulate a set.
	linkSet := make(map[string]*struct{})
	for _, link := range containerConfig.HostConfig.Links {
		containerRef_alias := strings.SplitN(link, ":", 2)
		containerIdName := strings.TrimSpace(containerRef_alias[0])
		containerId := containerReferenceToIdMap[containerIdName]
		linkSet[containerId] = nil
		if containerId != containerIdName {
			linkSet[containerId+":"+containerIdName] = nil
		}
		if len(containerRef_alias) > 1 {
			linkSet[containerId+":"+strings.TrimSpace(containerRef_alias[1])] = nil
		}
	}
	links := make([]string, len(linkSet))
	linksIndex := 0
	for containerId_alias := range linkSet {
		links[linksIndex] = containerId_alias
		linksIndex++
	}
	containerConfig.HostConfig.Links = links
	return nil

}
func getIDsFromContainerReferences(cluster cluster.Cluster, tenantId string, containerReferences []string) (map[string]string, error) {
	// containerReferences is a array of container references of the form long id, short id, or name
	containerReferenceToIdMap := make(map[string]string)
	for _, containerReference := range containerReferences {
		containerReferenceToIdMap[containerReference] = ""
	}
	// loop through all the containers to find the containerIds that at associated with the container references.
	// eligible containers must belong to the tenant
	for _, container := range cluster.Containers() {
		if container.Labels[headers.TenancyLabel] == tenantId {
			var err error
			var containerId string
			// look for containerReference found in volumes_from and links
			for _, containerReference := range containerReferences {
				if containerId, err = getContainerId(container, tenantId, containerReference); err == nil {
					containerReferenceToIdMap[containerReference] = containerId
					break
				}
			}

		}
	}
	// check that all container refences have been discovered.
	// if not return an error
	for _, containerReference := range containerReferences {
		if containerReferenceToIdMap[containerReference] == "" {
			err := fmt.Errorf(NOTAUTHORIZED_ERROR, containerReference)
			return nil, err
		}
	}

	return containerReferenceToIdMap, nil
}

func getContainerId(container *cluster.Container, tenantId string, containerReference string) (string, error) {
	if container.Labels[headers.TenancyLabel] != tenantId {
		return "", errors.New(CONTAINER_NOT_OWNED_INFO)
	}
	// check for long id
	if container.Info.ID == containerReference {
		return container.Info.ID, nil
	} else if containerReference == container.Labels[headers.OriginalNameLabel] {
		return container.Info.ID, nil
	} else {
		// check for name
		for _, name := range container.Names {
			if containerReference == name {
				return container.Info.ID, nil
			}
		}
	}
	// check for short id
	if strings.HasPrefix(container.Info.ID, containerReference) {
		return container.Info.ID, nil
	}
	return "", errors.New(CONTAINER_REFERENCE_NOT_FOR_CONTAINER_INFO)
}
