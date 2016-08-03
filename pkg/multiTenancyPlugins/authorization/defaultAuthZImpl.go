package authorization

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm/cluster"
	clusterParams "github.com/docker/swarm/cluster"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/headers"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/pluginAPI"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/utils"
	"github.com/gorilla/mux"
)

type DefaultAuthZImpl struct {
	nextHandler pluginAPI.Handler
}

func NewAuthorization(handler pluginAPI.Handler) pluginAPI.PluginAPI {
	authZ := &DefaultAuthZImpl{
		nextHandler: handler,
	}
	return authZ
}

func (defaultauthZ *DefaultAuthZImpl) Handle(command utils.CommandEnum, cluster cluster.Cluster, w http.ResponseWriter, r *http.Request, swarmHandler http.Handler) utils.ErrorInfo {
	log.Debug("Plugin AuthZ got command: " + command)
	var errInfo utils.ErrorInfo
	errInfo.Status = http.StatusBadRequest
	switch command {
	case utils.CONTAINER_CREATE:
		defer r.Body.Close()
		if reqBody, _ := ioutil.ReadAll(r.Body); len(reqBody) > 0 {
			var config clusterParams.ContainerConfig
			if err := json.NewDecoder(bytes.NewReader(reqBody)).Decode(&config); err != nil {
				errInfo.Err = err
				return errInfo
			}
			//Disallow a user to create the special labels we inject : headers.TenancyLabel
			if strings.Contains(string(reqBody), headers.TenancyLabel) == true {
				errInfo.Err = errors.New("Error, special label " + headers.TenancyLabel + " not allowed!")
				return errInfo
			}
			// network authorization
			if err := NetworkAuthorization(cluster, r, string(config.HostConfig.NetworkMode)); err != nil {
				errInfo.Err = err
				return errInfo
			}
			config.Config.Labels[headers.TenancyLabel] = r.Header.Get(headers.AuthZTenantIdHeaderName)
			//if err := hostFSMountCheck(r.Header.Get(headers.AuthZTenantIdHeaderName), oldconfig.HostConfig.Binds); err != nil {
			//	return err
			//}
			var buf bytes.Buffer
			if err := json.NewEncoder(&buf).Encode(config); err != nil {
				errInfo.Err = err
				return errInfo
			}
			r, _ = utils.ModifyRequest(r, bytes.NewReader(buf.Bytes()), "", "")
		}
		return defaultauthZ.nextHandler(command, cluster, w, r, swarmHandler)
		log.Debug("Returned from Swarm")
		//In case of container json - should record and clean - consider seperating..
	case utils.CONTAINER_START, utils.CONTAINER_STOP, utils.CONTAINER_RESTART, utils.CONTAINER_DELETE, utils.CONTAINER_WAIT, utils.CONTAINER_ARCHIVE, utils.CONTAINER_KILL, utils.CONTAINER_PAUSE, utils.CONTAINER_UNPAUSE, utils.CONTAINER_UPDATE, utils.CONTAINER_COPY, utils.CONTAINER_CHANGES, utils.CONTAINER_ATTACH, utils.CONTAINER_LOGS, utils.CONTAINER_TOP, utils.CONTAINER_STATS, utils.CONTAINER_EXEC:
		if !utils.IsResourceOwner(cluster, r.Header.Get(headers.AuthZTenantIdHeaderName), mux.Vars(r)["name"], "container") {
			errInfo.Err = errors.New(fmt.Sprint("status ", http.StatusNotFound, " HTTP error: No such container"))
			errInfo.Status = http.StatusNotFound
			return errInfo
		}
		return defaultauthZ.nextHandler(command, cluster, w, r, swarmHandler)

	case utils.CONTAINER_JSON:
		if !utils.IsResourceOwner(cluster, r.Header.Get(headers.AuthZTenantIdHeaderName), mux.Vars(r)["name"], "container") {
			errInfo.Err = errors.New(fmt.Sprint("status ", http.StatusNotFound, " HTTP error: No such container"))
			errInfo.Status = http.StatusNotFound
			return errInfo
		}
		rec := httptest.NewRecorder()
		if errInfo := defaultauthZ.nextHandler(command, cluster, rec, r, swarmHandler); errInfo.Err != nil {
			return errInfo
		}
		/*POST Swarm*/
		w.WriteHeader(rec.Code)
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}

		newBody := utils.CleanUpLabeling(r, rec)

		w.Write(newBody)

	case utils.JSON, utils.PS:
		//TODO - clean up code
		var v = url.Values{}
		mapS := map[string][]string{"label": {headers.TenancyLabel + "=" + r.Header.Get(headers.AuthZTenantIdHeaderName)}}
		filterJSON, _ := json.Marshal(mapS)
		v.Set("filters", string(filterJSON))
		var newQuery string
		if strings.Contains(r.URL.RequestURI(), "?") {
			newQuery = r.URL.RequestURI() + "&" + v.Encode()
		} else {
			newQuery = r.URL.RequestURI() + "?" + v.Encode()
		}
		log.Debug("New Query: ", newQuery)

		newReq, e1 := utils.ModifyRequest(r, nil, newQuery, "")
		if e1 != nil {
			log.Error(e1)
		}
		rec := httptest.NewRecorder()
		if errInfo := defaultauthZ.nextHandler(command, cluster, rec, newReq, swarmHandler); errInfo.Err != nil {
			return errInfo
		}
		//TODO - May decide to overrideSwarms handlers.getContainersJSON - this is Where to do it.
		/*POST Swarm*/
		w.WriteHeader(rec.Code)
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		newBody := utils.CleanUpLabeling(r, rec)
		w.Write(newBody)

	case utils.NETWORKS_LIST:
		rec := httptest.NewRecorder()
		if errInfo := defaultauthZ.nextHandler(command, cluster, rec, r, swarmHandler); errInfo.Err != nil {
			return errInfo
		}
		w.WriteHeader(rec.Code)
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		newBody := utils.FilterNetworks(r, rec)
		w.Write(newBody)

	case utils.NETWORK_INSPECT, utils.NETWORK_DELETE:
		if !utils.IsResourceOwner(cluster, r.Header.Get(headers.AuthZTenantIdHeaderName), mux.Vars(r)["networkid"], "network") {
			errInfo.Err = errors.New("Not authorized or no such network!")
			return errInfo
		}
		return defaultauthZ.nextHandler(command, cluster, w, r, swarmHandler)

	case utils.NETWORK_CONNECT, utils.NETWORK_DISCONNECT:
		if errInfo := ConnectDisconnect(cluster, r); errInfo.Err != nil {
			return errInfo
		}
		return defaultauthZ.nextHandler(command, cluster, w, r, swarmHandler)

	case utils.INFO, utils.NETWORK_CREATE, utils.EVENTS, utils.IMAGES_JSON, utils.IMAGE_PULL, utils.IMAGE_SEARCH, utils.IMAGE_HISTORY, utils.IMAGE_JSON:
		return defaultauthZ.nextHandler(command, cluster, w, r, swarmHandler)

	case utils.EXEC_START, utils.EXEC_RESIZE, utils.EXEC_JSON:
		if !utils.VerifyExecContainerTenant(cluster, r.Header.Get(headers.AuthZTenantIdHeaderName), r) {
			errInfo.Err = errors.New("Not Authorized!")
			return errInfo
		}
		return defaultauthZ.nextHandler(command, cluster, w, r, swarmHandler)
	case utils.VOLUME_DELETE:
		if errInfo := volumeOwnershipCheck(r, "name"); errInfo.Err != nil {
			return errInfo
		}
		return defaultauthZ.nextHandler(command, cluster, w, r, swarmHandler)
	case utils.VOLUME_INSPECT:
		if errInfo := volumeOwnershipCheck(r, "volumename"); errInfo.Err != nil {
			return errInfo
		}
		return defaultauthZ.nextHandler(command, cluster, w, r, swarmHandler)
	case utils.VOLUMES_LIST:
		rec := httptest.NewRecorder()
		if errInfo := defaultauthZ.nextHandler(command, cluster, rec, r, swarmHandler); errInfo.Err != nil {
			return errInfo
		}
		w.WriteHeader(rec.Code)
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		newBody := filterVolumes(r, rec)
		w.Write(newBody)
	case utils.VOLUME_CREATE:
		return defaultauthZ.nextHandler(command, cluster, w, r, swarmHandler)

	//Always allow or not?
	default:
		if !utils.IsResourceOwner(cluster, r.Header.Get(headers.AuthZTenantIdHeaderName), mux.Vars(r)["name"], "container") {
			errInfo.Err = errors.New(fmt.Sprint("status ", http.StatusNotFound, " HTTP error: No such resource"))
			errInfo.Status = http.StatusNotFound
			return errInfo
		}
	}
	errInfo.Err = nil
	return errInfo
}
