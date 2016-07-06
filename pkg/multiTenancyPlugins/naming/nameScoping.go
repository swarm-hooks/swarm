package namescoping

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm/cluster"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/headers"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/pluginAPI"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/utils"
	"github.com/gorilla/mux"
	"github.com/samalba/dockerclient"
)

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


func getContainerID(cluster cluster.Cluster, r *http.Request, containerName string) string {
	tenantId := r.Header.Get(headers.AuthZTenantIdHeaderName)
	for _, container := range cluster.Containers() {
		if container.Info.ID == containerName {
			//Match by Full Id
			return container.Info.ID
		} else {
			for _, name := range container.Names {
				if (containerName == name || containerName == container.Labels[headers.OriginalNameLabel]) && container.Labels[headers.TenancyLabel] == tenantId {
					//Match by Name
					return container.Info.ID
				}
			}
		}
		if strings.HasPrefix(container.Info.ID, containerName) {
			//Match by short ID
			return container.Info.ID
		}
	}
	return containerName
}


func getNetworkID(cluster cluster.Cluster, r *http.Request, networkId string) string {
	tenantId := r.Header.Get(headers.AuthZTenantIdHeaderName)
	for _, network := range cluster.Networks() {
		if network.ID == networkId {
			//Match by Full ID.
			return network.ID
		} else {
			if network.Name == tenantId + networkId {
				//Match by name. Replace by full ID.
				return network.ID
			}
		}
		if strings.HasPrefix(network.ID, networkId) {
			//Match by short id. Replace by full ID.
			return network.ID
		}
	}
	return networkId
}

//Handle authentication on request and call next plugin handler.
func (nameScoping *DefaultNameScopingImpl) Handle(command utils.CommandEnum, cluster cluster.Cluster, w http.ResponseWriter, r *http.Request, swarmHandler http.Handler) error {
	log.Debug("Plugin nameScoping Got command: " + command)
	switch command {
	case utils.CONTAINER_CREATE:
		if "" != r.URL.Query().Get("name") {
			defer r.Body.Close()
			if reqBody, _ := ioutil.ReadAll(r.Body); len(reqBody) > 0 {
				var newQuery string
				var buf bytes.Buffer
				var containerConfig dockerclient.ContainerConfig

				if err := json.NewDecoder(bytes.NewReader(reqBody)).Decode(&containerConfig); err != nil {
					return err
				}

				log.Debug("Postfixing name with tenantID...")
				newQuery = strings.Replace(r.RequestURI, r.URL.Query().Get("name"), r.URL.Query().Get("name")+r.Header.Get(headers.AuthZTenantIdHeaderName), 1)
				//Disallow a user to create the special labels we inject : headers.OriginalNameLabel
				res := strings.Contains(string(reqBody), headers.OriginalNameLabel)
				if res == true {
					errorMessage := "Error, special label " + headers.OriginalNameLabel + " disallowed!"
					return errors.New(errorMessage)
				}
				containerConfig.Labels[headers.OriginalNameLabel] = r.URL.Query().Get("name")

				if err := json.NewEncoder(&buf).Encode(containerConfig); err != nil {
					return err
				}

				r, _ = utils.ModifyRequest(r, bytes.NewReader(buf.Bytes()), newQuery, "")
			}
		}
		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)

	//Find the container and replace the name with ID
	case utils.CONTAINER_JSON:
		if resourceName := mux.Vars(r)["name"]; resourceName != "" {
			containerName := mux.Vars(r)["name"]
			conatinerID := getContainerID(cluster, r, containerName)
			mux.Vars(r)["name"] = conatinerID
			r.URL.Path = strings.Replace(r.URL.Path, containerName, conatinerID, 1)
			return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)
		} else {
			log.Debug("What now?")
		}
	case utils.CONTAINER_START, utils.CONTAINER_STOP, utils.CONTAINER_RESTART, utils.CONTAINER_DELETE, utils.CONTAINER_WAIT, utils.CONTAINER_ARCHIVE, utils.CONTAINER_KILL, utils.CONTAINER_PAUSE, utils.CONTAINER_UNPAUSE, utils.CONTAINER_UPDATE, utils.CONTAINER_COPY, utils.CONTAINER_CHANGES, utils.CONTAINER_ATTACH, utils.CONTAINER_LOGS, utils.CONTAINER_TOP, utils.CONTAINER_STATS:
		containerName := mux.Vars(r)["name"]
		conatinerID := getContainerID(cluster, r, containerName)
		mux.Vars(r)["name"] = conatinerID
		r.URL.Path = strings.Replace(r.URL.Path, containerName, conatinerID, 1)
		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)

	case utils.NETWORK_CONNECT, utils.NETWORK_DISCONNECT:
		case c.NETWORK_CONNECT, c.NETWORK_DISCONNECT:
		if err := ConnectDisconnect(cluster, r); err != nil {
			return err
		}
		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)
		
	case c.NETWORK_CREATE:
		if err := CreateNetwork(cluster, r); err != nil {
			return err
		}
		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)
		
	case c.NETWORK_INSPECT, c.NETWORK_DELETE:
		DeleteInspect(cluster, r)
		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)
		
	case utils.PS, utils.JSON, utils.NETWORKS_LIST, utils.INFO, utils.EVENTS, utils.IMAGES_JSON:
		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)
		
	default:

	}
	return nil
}
