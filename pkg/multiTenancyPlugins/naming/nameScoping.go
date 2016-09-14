package namescoping

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	log "github.com/Sirupsen/logrus"
	apitypes "github.com/docker/engine-api/types"
	"github.com/docker/swarm/cluster"
	clusterParams "github.com/docker/swarm/cluster"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/headers"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/pluginAPI"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/utils"
	"github.com/gorilla/mux"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
)

const NOTAUTHORIZED_ERROR = "No such container or the user is not authorized for this container: %s."
const CONTAINER_NOT_OWNED_INFO = "container not owned by current tenant info."
const CONTAINER_REFERENCE_NOT_FOR_CONTAINER_INFO = "container reference does not match this container info."

type DefaultNameScopingImpl struct {
	nextHandler pluginAPI.Handler
}

func NewNameScoping(handler pluginAPI.Handler) pluginAPI.PluginAPI {
	nameScoping := &DefaultNameScopingImpl{
		nextHandler: handler,
	}
	return nameScoping
}

//Handle authentication on request and call next plugin handler.
func (nameScoping *DefaultNameScopingImpl) Handle(command utils.CommandEnum, cluster cluster.Cluster, w http.ResponseWriter, r *http.Request, swarmHandler http.Handler) utils.ErrorInfo {
	log.Debug("Plugin nameScoping Got command: " + command)
	var errInfo utils.ErrorInfo
	errInfo.Status = http.StatusBadRequest
	switch command {
	case utils.CONTAINER_CREATE:
		var newQuery string
		var buf bytes.Buffer
		var config clusterParams.ContainerConfig
		var reqBody []byte
		defer r.Body.Close()
		if reqBody, _ = ioutil.ReadAll(r.Body); len(reqBody) > 0 {
			if err := json.NewDecoder(bytes.NewReader(reqBody)).Decode(&config); err != nil {
				log.Error(err)
				errInfo.Err = err
				return errInfo
			}
		}
		if "" != r.URL.Query().Get("name") {
			log.Debug("Postfixing name with tenantID...")
			newQuery = strings.Replace(r.RequestURI, r.URL.Query().Get("name"), r.URL.Query().Get("name")+r.Header.Get(headers.AuthZTenantIdHeaderName), 1)
			config.Config.Labels[headers.OriginalNameLabel] = r.URL.Query().Get("name")
		}
		// Replace network reference with network ID, force Network-scoped alias for DNS use.
		config = handleNetworkParameters(cluster, r, config)
		if err := CheckContainerReferences(cluster, r.Header.Get(headers.AuthZTenantIdHeaderName), &config); err != nil {
			log.Error(err)
			errInfo.Err = err
			return errInfo
		}
		config.HostConfig.Binds = getVolumeBindings(r, config.HostConfig.Binds)
		if len(reqBody) > 0 {
			if err := json.NewEncoder(&buf).Encode(config); err != nil {
				log.Error(err)
				errInfo.Err = err
				return errInfo
			}
			r, _ = utils.ModifyRequest(r, bytes.NewReader(buf.Bytes()), newQuery, "")
		}
		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)

	case utils.CONTAINER_JSON, utils.CONTAINER_START, utils.CONTAINER_STOP, utils.CONTAINER_RESTART, utils.CONTAINER_DELETE, utils.CONTAINER_WAIT, utils.CONTAINER_ARCHIVE, utils.CONTAINER_KILL, utils.CONTAINER_PAUSE, utils.CONTAINER_UNPAUSE, utils.CONTAINER_UPDATE, utils.CONTAINER_COPY, utils.CONTAINER_CHANGES, utils.CONTAINER_ATTACH, utils.CONTAINER_LOGS, utils.CONTAINER_TOP, utils.CONTAINER_STATS, utils.CONTAINER_EXEC, utils.CONTAINER_EXPORT, utils.CONTAINER_IMPORT:
		containerName := mux.Vars(r)["name"]
		conatinerID, err := utils.GetContainerID(cluster, r.Header.Get(headers.AuthZTenantIdHeaderName), containerName)
		if err != nil {
			log.Error(err)
			errInfo.Err = errors.New(fmt.Sprint("status ", http.StatusNotFound, " HTTP error: No such resource"))
			errInfo.Status = http.StatusNotFound
			return errInfo
		}
		mux.Vars(r)["name"] = conatinerID
		r.URL.Path = strings.Replace(r.URL.Path, containerName, conatinerID, 1)
		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)

	case utils.NETWORK_CONNECT, utils.NETWORK_DISCONNECT:
		if err := ConnectDisconnect(cluster, r); err != nil {
			errInfo.Err = err
			return errInfo
		}
		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)

	case utils.NETWORK_CREATE:
		if err := CreateNetwork(cluster, r); err != nil {
			errInfo.Err = err
			return errInfo
		}
		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)

	case utils.NETWORK_DELETE:
		DeleteInspect(cluster, r)
		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)

	case utils.NETWORK_INSPECT:
		networkName := mux.Vars(r)["networkid"]
		DeleteInspect(cluster, r)
		responseRecorder := httptest.NewRecorder()
		if errInfo := nameScoping.nextHandler(command, cluster, responseRecorder, r, swarmHandler); errInfo.Err != nil {
			return errInfo
		}
		// Show original names in the response
		w.WriteHeader(responseRecorder.Code)
		for k, v := range responseRecorder.Header() {
			w.Header()[k] = v
		}
		newBody := cleanUpNames(responseRecorder, networkName)
		w.Write(newBody)

	case utils.PS, utils.JSON, utils.NETWORKS_LIST, utils.INFO, utils.EVENTS, utils.IMAGES_JSON, utils.EXEC_START, utils.EXEC_RESIZE, utils.EXEC_JSON, utils.IMAGE_PULL, utils.IMAGE_SEARCH, utils.IMAGE_HISTORY, utils.IMAGE_JSON, utils.VERSION:
		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)

	case utils.VOLUME_CREATE:
		var err error
		defer r.Body.Close()
		// append volume name with tenant name.
		if reqBody, _ := ioutil.ReadAll(r.Body); len(reqBody) > 0 {
			var request apitypes.VolumeCreateRequest
			if err = json.NewDecoder(bytes.NewReader(reqBody)).Decode(&request); err != nil {
				errInfo.Err = err
				return errInfo
			}
			if request.Name, err = nameScopeVolumeName(r, request.Name); err != nil {
				errInfo.Err = err
				return errInfo
			}
			var buf bytes.Buffer
			if err = json.NewEncoder(&buf).Encode(request); err != nil {
				errInfo.Err = err
				return errInfo
			}
			r = closeBody(r, bytes.NewReader(buf.Bytes()))
		}
		if errInfo := doReq(nameScoping, command, cluster, w, r, swarmHandler); errInfo.Err != nil {
			return errInfo
		}

	case utils.VOLUME_INSPECT:
		updateVolumeName(r, "volumename")
		if errInfo := doReq(nameScoping, command, cluster, w, r, swarmHandler); errInfo.Err != nil {
			return errInfo
		}
	case utils.VOLUME_DELETE:
		updateVolumeName(r, "name")
		if errInfo := doReq(nameScoping, command, cluster, w, r, swarmHandler); errInfo.Err != nil {
			return errInfo
		}
	case utils.VOLUMES_LIST:
		if errInfo := doReq(nameScoping, command, cluster, w, r, swarmHandler); errInfo.Err != nil {
			return errInfo
		}

	default:

	}
	errInfo.Err = nil
	return errInfo
}

func CheckContainerReferences(cluster cluster.Cluster, tenantId string, config *clusterParams.ContainerConfig) error {
	// create arrays of container references to pass to getIDsFromContainerReferences

	// create volumes-from array
	volumesFromRef := make([]string, len(config.HostConfig.VolumesFrom))
	for i, volume_from := range config.HostConfig.VolumesFrom {
		// link in form [container reference]:[alias]
		// : and alias are optional
		containerRef_mode := strings.SplitN(volume_from, ":", 2)
		volumesFromRef[i] = strings.TrimSpace(containerRef_mode[0])
	}

	// create links array
	linkContainerRefs := make([]string, len(config.HostConfig.Links))
	for i, link := range config.HostConfig.Links {
		// link in form [container reference]:[alias]
		// : and alias are optional
		containerRef_alias := strings.SplitN(link, ":", 2)
		linkContainerRefs[i] = strings.TrimSpace(containerRef_alias[0])
	}

	var err error
	var containerReferenceToIdMap map[string]string
	containerRefs := make([]string, 0)
	containerRefs = append(containerRefs, volumesFromRef...)
	containerRefs = append(containerRefs, linkContainerRefs...)
	if containerReferenceToIdMap, err = getIDsFromContainerReferences(cluster, tenantId, containerRefs); err != nil {
		log.Debugf("CheckContainerReferences err: %+v", err)
		return err
	}
	// ******************update containerConfig******************
	// update VolumesFrom
	for i, volume_from := range config.HostConfig.VolumesFrom {
		containerRef_mode := strings.SplitN(volume_from, ":", 2)
		if len(containerRef_mode) > 1 {
			config.HostConfig.VolumesFrom[i] = containerReferenceToIdMap[containerRef_mode[0]] + ":" + containerRef_mode[1]
		} else {
			config.HostConfig.VolumesFrom[i] = containerReferenceToIdMap[containerRef_mode[0]]
		}
	}

	// update links
	// We want to create an array of links with no duplicates.  Ideally, to do this we would use a set however
	// go does not support a native set structure.  Rather we use a map, named linkSet, to accumulated non duplicate links.
	// Only the keys are important in linkSet;  the values are meaningless.
	// Once linkSet is created we generate the links array from linkSet.
	// We want to generate a super set of links with no duplicates.  However go does not support a native set,
	// do we use map that points to an empty structure to simulate a set.
	linkSet := make(map[string]*struct{})
	for _, link := range config.HostConfig.Links {
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
	config.HostConfig.Links = links
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
	foundCntDown := len(containerReferences)
Loop:
	for _, container := range cluster.Containers() {
		log.Debugf("getIDsFromContainerReferences check container: %+v", container)

		if container.Labels[headers.TenancyLabel] == tenantId {
			var err error
			var containerId string
			// look for containerReference found in volumes_from and links
			for _, containerReference := range containerReferences {
				if containerId, err = getID(container, tenantId, containerReference); err == nil {
					containerReferenceToIdMap[containerReference] = containerId
					foundCntDown--
					if foundCntDown == 0 {
						break Loop
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

	return containerReferenceToIdMap, nil
}

func getID(container *cluster.Container, tenantId string, containerReference string) (string, error) {
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
			if containerReference == strings.TrimPrefix(name, "/") {
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

// send request to next handler in record mode.  When it returns remove traces of tenant id in response
func doReq(nameScoping *DefaultNameScopingImpl, command utils.CommandEnum, cluster cluster.Cluster, w http.ResponseWriter, r *http.Request, swarmHandler http.Handler) utils.ErrorInfo {
	var errorInfo utils.ErrorInfo
	rec := httptest.NewRecorder()
	if errorInfo := nameScoping.nextHandler(command, cluster, rec, r, swarmHandler); errorInfo.Err != nil {
		return errorInfo
	}
	/*POST Swarm*/
	w.WriteHeader(rec.Code)
	for k, v := range rec.Header() {
		w.Header()[k] = v
	}
	newBody := bytes.Replace(rec.Body.Bytes(), []byte(r.Header.Get(headers.AuthZTenantIdHeaderName)), []byte(""), -1)
	w.Write(newBody)
	return errorInfo

}
func closeBody(r *http.Request, body io.Reader) *http.Request {
	rc, ok := body.(io.ReadCloser)
	if !ok && body != nil {
		rc = ioutil.NopCloser(body)
		r.Body = rc
	}
	return r
}
