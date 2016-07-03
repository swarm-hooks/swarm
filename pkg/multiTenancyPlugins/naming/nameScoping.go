package namescoping

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	log "github.com/Sirupsen/logrus"
	apitypes "github.com/docker/engine-api/types"
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

type affinityType int

const (
	AFFINITY_CONTAINER affinityType = 1 + iota
	AFFINITY_LABEL
)

type affinityRefType struct {
	affinityElementType affinityType
	envElementIndex     int
}

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

func uniquelyIdentifyContainer(cluster cluster.Cluster, r *http.Request, w http.ResponseWriter) {
	resourceName := mux.Vars(r)["name"]
	tenantId := r.Header.Get(headers.AuthZTenantIdHeaderName)
Loop:
	for _, container := range cluster.Containers() {
		if container.Info.ID == resourceName {
			//Match by Full Id - Do nothing
			break
		} else {
			for _, name := range container.Names {
				name := strings.TrimPrefix(name, "/")
				if (resourceName == name || resourceName == container.Labels[headers.OriginalNameLabel]) && container.Labels[headers.TenancyLabel] == tenantId {
					//Match by Name - Replace to full ID
					mux.Vars(r)["name"] = container.Info.ID
					r.URL.Path = strings.Replace(r.URL.Path, resourceName, container.Info.ID, 1)
					break Loop
				}
			}
		}
		if strings.HasPrefix(container.Info.ID, resourceName) {
			mux.Vars(r)["name"] = container.Info.ID
			r.URL.Path = strings.Replace(r.URL.Path, resourceName, container.Info.ID, 1)
			break
		}
	}
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
			uniquelyIdentifyContainer(cluster, r, w)
			return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)
		} else {
			log.Debug("What now?")
		}
	case utils.CONTAINER_START, utils.CONTAINER_STOP, utils.CONTAINER_RESTART, utils.CONTAINER_DELETE, utils.CONTAINER_WAIT, utils.CONTAINER_ARCHIVE, utils.CONTAINER_KILL, utils.CONTAINER_PAUSE, utils.CONTAINER_UNPAUSE, utils.CONTAINER_UPDATE, utils.CONTAINER_COPY, utils.CONTAINER_CHANGES, utils.CONTAINER_ATTACH, utils.CONTAINER_LOGS, utils.CONTAINER_TOP, utils.CONTAINER_STATS:
		uniquelyIdentifyContainer(cluster, r, w)
		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)
	case utils.NETWORK_CREATE:
		defer r.Body.Close()
		if reqBody, _ := ioutil.ReadAll(r.Body); len(reqBody) > 0 {

			var request apitypes.NetworkCreate
			if err := json.NewDecoder(bytes.NewReader(reqBody)).Decode(&request); err != nil {
				log.Error(err)
				return nil
			}
			request.Name = r.Header.Get(headers.AuthZTenantIdHeaderName) + request.Name
			var buf bytes.Buffer
			if err := json.NewEncoder(&buf).Encode(request); err != nil {
				log.Error(err)
				return nil
			}
			r, _ = utils.ModifyRequest(r, bytes.NewReader(buf.Bytes()), "", "")
		}
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

	// create affinity array
	affinityFromEnvMap := make(map[string]*affinityRefType)
	env := containerConfig.Env
	for envElementIndex, envElement := range env {
		if strings.HasPrefix(envElement, "affinity:") {
			if strings.HasPrefix(envElement, "affinity:image==") {
				continue // ignore affinity for images
			} else if strings.HasPrefix(envElement, "affinity:container==") {
				containerRefIndex := strings.Index(envElement, "affinity:container==") + len("affinity:container==")
				containerRef := envElement[containerRefIndex:]
				affinityFromEnvMap[containerRef] = &affinityRefType{AFFINITY_CONTAINER, envElementIndex}
			} else { // affinity:<label>:<value>
				labelRefIndex := strings.Index(envElement, "affinity:") + len("affinity:")
				containerRef := envElement[labelRefIndex:]
				affinityFromEnvMap[containerRef] = &affinityRefType{AFFINITY_LABEL, envElementIndex}

			}
		}
	}

	// create links array
	linkContainerRefs := make([]string, 0)
	for _, link := range containerConfig.HostConfig.Links {
		// link in form [container reference]:[alias]
		// : and alias are optional
		containerRef_alias := strings.SplitN(link, ":", 2)
		// linkSplit[0] == container reference
		linkContainerRefs = append(linkContainerRefs, strings.TrimSpace(containerRef_alias[0]))
	}

	var err error
	var containerReferenceToIdMap map[string]string
	containerRefs := make([]string, 0)
	containerRefs = append(containerRefs, containerConfig.HostConfig.VolumesFrom...)
	containerRefs = append(containerRefs, linkContainerRefs...)
	if containerReferenceToIdMap, err = getIDsFromContainerReferences(cluster, tenantId, containerRefs, affinityFromEnvMap); err != nil {
		return err
	}
	// ******************update containerConfig******************
	// update VolumesFrom
	for i, k := range containerConfig.HostConfig.VolumesFrom {
		containerConfig.HostConfig.VolumesFrom[i] = containerReferenceToIdMap[k]
	}

	// update affinity
	for containerRef, affinityRef := range affinityFromEnvMap {
		if affinityRef.affinityElementType == AFFINITY_CONTAINER {
			containerConfig.Env[affinityRef.envElementIndex] = "affinity:container==" + containerReferenceToIdMap[containerRef]
		} else {
			containerConfig.Env[affinityRef.envElementIndex] = "affinity:container==" + containerReferenceToIdMap[containerRef]
		}
	}

	// update links
	// We want to create an array of links with no duplicates.  Ideally, to do this we would use a set however
	// go does not support a native set structure.  Rather we use a map, named linkSet, to accumulated non duplicate links.
	// Only the keys are important in linkSet;  the values are meaningless.
	// Once linkSet is created we generate the links array from linkSet.
	links := make([]string, 0)
	// We want to generate a set of links with no dugo does not support a native set, s
	linkSet := make(map[string]string)
	for _, link := range containerConfig.HostConfig.Links {
		containerRef_alias := strings.SplitN(link, ":", 2)
		containerIdName := strings.TrimSpace(containerRef_alias[0])
		containerId := containerReferenceToIdMap[containerIdName]
		linkSet[containerId] = ""
		if containerId != containerIdName {
			linkSet[containerId+":"+containerIdName] = ""
		}
		if len(containerRef_alias) > 1 {
			linkSet[containerId+":"+strings.TrimSpace(containerRef_alias[1])] = ""
		}
	}
	for containerId_alias := range linkSet {
		links = append(links, containerId_alias)
	}
	containerConfig.HostConfig.Links = links
	return nil

}
func getIDsFromContainerReferences(cluster cluster.Cluster, tenantId string, containerReferences []string, affinityFromEnvMap map[string]*affinityRefType) (map[string]string, error) {
	// containerReferences is a array of container references of the form long id, short id, or name
	// containerReferenceToIdMap is a map from the containerReferences to the containterId
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
			// look for containerReferences found in affinity
			for containerReference, affinityRef := range affinityFromEnvMap {
				if affinityRef.affinityElementType == AFFINITY_CONTAINER {
					if containerId, err = getContainerId(container, tenantId, containerReference); err == nil {
						containerReferenceToIdMap[containerReference] = containerId
						break
					}

				} else {
					if containerId, err = getIDFromContainerLabel(container, tenantId, containerReference); err == nil {
						containerReferenceToIdMap[containerReference] = containerId
						break
					}
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
	for containerReference := range affinityFromEnvMap {
		if containerReferenceToIdMap[containerReference] == "" {
			err := fmt.Errorf(NOTAUTHORIZED_ERROR, containerReference)
			return nil, err
		}
	}

	return containerReferenceToIdMap, nil
}

func getIDFromContainerLabel(container *cluster.Container, tenantId string, affinityLabelValue string) (string, error) {
	if container.Labels[headers.TenancyLabel] != tenantId {
		return "", errors.New(CONTAINER_NOT_OWNED_INFO)
	}
	// affinityLabelValue is in the form label==value
	label_value := strings.Split(affinityLabelValue, "==")
	for label, value := range container.Config.Labels {
		if label == label_value[0] && value == label_value[1] {
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
