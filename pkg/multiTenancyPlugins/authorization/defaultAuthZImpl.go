package authorization

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/docker/swarm/pkg/multiTenancyPlugins/pluginAPI"
	"github.com/samalba/dockerclient"
	log "github.com/Sirupsen/logrus"
	apitypes "github.com/docker/engine-api/types"
	"github.com/docker/swarm/cluster"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/headers"
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

func (defaultauthZ *DefaultAuthZImpl) Handle(command string, cluster cluster.Cluster, w http.ResponseWriter, r *http.Request, swarmHandler http.Handler) error {
	log.Debug("Plugin AuthZ got command: " + command)
	switch command {
	case "containercreate":
		defer r.Body.Close()
		if reqBody, _ := ioutil.ReadAll(r.Body); len(reqBody) > 0 {
			var containerConfig dockerclient.ContainerConfig
			if err := json.NewDecoder(bytes.NewReader(reqBody)).Decode(&containerConfig); err != nil {
				return err
			}
			// network authorization
			if !utils.IsResourceOwner(cluster, r.Header.Get(headers.AuthZTenantIdHeaderName), containerConfig.HostConfig.NetworkMode, "network") {
				return errors.New("Not authorized or no such network!")
			}
			containerConfig.Labels[headers.TenancyLabel] = r.Header.Get(headers.AuthZTenantIdHeaderName)

			var buf bytes.Buffer
			if err := json.NewEncoder(&buf).Encode(containerConfig); err != nil {
				return err
			}

			r, _ = utils.ModifyRequest(r, bytes.NewReader(buf.Bytes()), "", "")
		}	
		swarmHandler.ServeHTTP(w, r)

		//In case of container json - should record and clean - consider seperating..
	case "containerstart", "containerstop", "containerdelete", "containerkill", "containerpause", "containerunpause", "containerupdate", "containercopy", "containerattach", "containerlogs":
		if !utils.IsResourceOwner(cluster, r.Header.Get(headers.AuthZTenantIdHeaderName), mux.Vars(r)["name"], "container") {
			return errors.New("Not Authorized or no such resource!")
		}
		swarmHandler.ServeHTTP(w, r)

	case "containerjson":
		if !utils.IsResourceOwner(cluster, r.Header.Get(headers.AuthZTenantIdHeaderName), mux.Vars(r)["name"], "container") {
			return errors.New("Not Authorized or no such resource!")
		}
		rec := httptest.NewRecorder()
		swarmHandler.ServeHTTP(rec, r)
		/*POST Swarm*/
		w.WriteHeader(rec.Code)
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}

		newBody := utils.CleanUpLabeling(r, rec)

		w.Write(newBody)

	case "listContainers":
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

		//TODO - May decide to overrideSwarms handlers.getContainersJSON - this is Where to do it.
		swarmHandler.ServeHTTP(rec, newReq)

		/*POST Swarm*/
		w.WriteHeader(rec.Code)
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}

		newBody := utils.CleanUpLabeling(r, rec)

		w.Write(newBody)

	case "listNetworks":
		rec := httptest.NewRecorder()
		swarmHandler.ServeHTTP(rec, r)

		w.WriteHeader(rec.Code)
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		newBody := utils.FilterNetworks(r, rec)
		w.Write(newBody)
		
	case "deleteNetwork", "inspectNetwork":
		if !utils.IsResourceOwner(cluster, r.Header.Get(headers.AuthZTenantIdHeaderName), mux.Vars(r)["networkid"], "network") {
			return errors.New("Not authorized or no such network!")
		}	
		swarmHandler.ServeHTTP(w, r)
	case "connectNetwork", "disconnectNetwork":
		if !utils.IsResourceOwner(cluster, r.Header.Get(headers.AuthZTenantIdHeaderName), mux.Vars(r)["networkid"], "network") {
			return errors.New("Not authorized or no such network!")
		}
		defer r.Body.Close()
		if reqBody, _ := ioutil.ReadAll(r.Body); len(reqBody) > 0 {
				var request apitypes.NetworkConnect
				if err := json.NewDecoder(bytes.NewReader(reqBody)).Decode(&request); err != nil {
         			return err
				}				
				if !utils.IsResourceOwner(cluster, r.Header.Get(headers.AuthZTenantIdHeaderName), request.Container, "container") {
					return errors.New("Not Authorized or no such resource!")
				}			
				var buf bytes.Buffer
				if err := json.NewEncoder(&buf).Encode(request); err != nil {
					return err
				}								
				r, _ = utils.ModifyRequest(r, bytes.NewReader(buf.Bytes()), "", "")			
		}
		swarmHandler.ServeHTTP(w, r)
	case "clusterInfo", "createNetwork":
		swarmHandler.ServeHTTP(w, r)	
	//Always allow or not?
	default:
		if !utils.IsResourceOwner(cluster, r.Header.Get(headers.AuthZTenantIdHeaderName), mux.Vars(r)["name"], "container") {
			return errors.New("Not Authorized or no such resource!")
		}
	}
	return nil
}
