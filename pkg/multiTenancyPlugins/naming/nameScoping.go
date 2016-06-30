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

	case utils.PS, utils.JSON, utils.NETWORKS_LIST, utils.INFO, utils.EVENTS, utils.IMAGES_JSON:
		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)

	default:

	}
	return nil
}

func CheckContainerReferences(cluster cluster.Cluster, tenantId string, containerConfig *dockerclient.ContainerConfig) error {
	log.Debugf("CheckContainerReferences containerConfig: %+v", containerConfig)
	// create arrays of container references to pass to getIDsFromContainerReferences
	// create volumesFrom array
	volumesFrom := containerConfig.HostConfig.VolumesFrom

	// create affinity array
	affinityLabelRefs := make([]string, 0)
	affinityContainerRefs := make([]string, 0)
	affinityLabelIndexs := make([]int, 0)
	affinityContainerIndexs := make([]int, 0)

	env := containerConfig.Env
	for i, e := range env {
		if strings.HasPrefix(e, "affinity:") {
			if strings.HasPrefix(e, "affinity:image==") {
				break // ignore affinity for images
			} else if strings.HasPrefix(e, "affinity:container==") {
				containerRefIndex := strings.Index(e, "affinity:container==") + len("affinity:container==")
				affinityContainerRefs = append(affinityContainerRefs, e[containerRefIndex:])
				affinityContainerIndexs = append(affinityContainerIndexs,i)
			} else { // affinity:<label>:<value>
				labelRefIndex := strings.Index(e, "affinity:") + len("affinity:")
				affinityLabelRefs = append(affinityLabelRefs, e[labelRefIndex:])
				affinityLabelIndexs = append(affinityLabelIndexs,i)
			}
		}
	}

	// create links array
	links := make([]string, 0)
	for _, link := range containerConfig.HostConfig.Links {
		linkSplit := strings.SplitN(link, ":", 2)
		links = append(links, strings.TrimSpace(linkSplit[0]))
	}

	var m map[string]string
	var err error
	containerRefs := make([]string, 0)
	containerRefs = append(containerRefs, affinityContainerRefs...)
	containerRefs = append(containerRefs, volumesFrom...)
	containerRefs = append(containerRefs, links...)
	if m, err = getIDsFromContainerReferences(cluster, tenantId, containerRefs, affinityLabelRefs); err != nil {
		return err
	}
	// update containerConfig
	// update VolumesFrom
	for i, k := range containerConfig.HostConfig.VolumesFrom {
		containerConfig.HostConfig.VolumesFrom[i] = m[k]
	}
	// update affinity
	for i,k := range affinityContainerRefs {
		containerConfig.Env[affinityContainerIndexs[i]] = "affinity:container==" + m[k]
	}
	for i,k := range affinityLabelRefs {
		containerConfig.Env[affinityLabelIndexs[i]] = "affinity:container==" + m[k]
	}

	// update links
	links = make([]string, 0)
	linkSet := make(map[string]bool)
	for _, link := range containerConfig.HostConfig.Links {
		linkSplit := strings.SplitN(link, ":", 2)
		containerIdName := strings.TrimSpace(linkSplit[0])
		containerId := m[containerIdName]
		if containerId != containerIdName {
			linkSet[containerId+":"+containerIdName] = true
		}
		if len(linkSplit) > 1 {
			linkSet[containerId+":"+strings.TrimSpace(linkSplit[1])] = true
		}
		for k := range linkSet {
			links = append(links, k)
		}
	}
	containerConfig.HostConfig.Links = links
	return nil

}
func getIDsFromContainerReferences(cluster cluster.Cluster, tenantId string, containerReferences []string, affinityLabelReferences []string) (map[string]string, error) {
	// containerReferences is a array of container references of the form long id, short id, or name
	m := make(map[string]string)
	for _, containerReference := range containerReferences {
		m[containerReference] = ""
	}
	for _, container := range cluster.Containers() {
		if container.Labels[headers.TenancyLabel] == tenantId {
			var err error
			var containerId string
			for _, containerReference := range containerReferences {
				if containerId, err = getContainerId(container, tenantId, containerReference); err == nil {
					m[containerReference] = containerId
					break
				}
			}
			for _, containerReference := range affinityLabelReferences {
				if containerId, err = getIDFromContainerLabel(container, tenantId, containerReference); err == nil {
					m[containerReference] = containerId
					break
				}
			}
		}
	}
	for _, containerReference := range containerReferences {
		if m[containerReference] == "" {
			err := fmt.Errorf(NOTAUTHORIZED_ERROR, containerReference)
			return nil, err
		}
	}
	for _, containerReference := range affinityLabelReferences {
		if m[containerReference] == "" {
			err := fmt.Errorf(NOTAUTHORIZED_ERROR, containerReference)
			return nil, err
		}
	}

	return m, nil
}

func getIDFromContainerLabel(container *cluster.Container, tenantId string, affinityLabelValue string) (string, error) {
	if container.Labels[headers.TenancyLabel] != tenantId {
		return "", errors.New(CONTAINER_NOT_OWNED_INFO)
	}
	// affinityLabelValue is in the form label==value
	kv := strings.Split(affinityLabelValue, "==")
	for k, v := range container.Config.Labels {
		if k == kv[0] && v == kv[1] {
			return container.Info.ID, nil
		}
	}
	return "", errors.New(CONTAINER_REFERENCE_NOT_FOR_CONTAINER_INFO)
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
