package quota

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm/cluster"
	clusterParams "github.com/docker/swarm/cluster"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/headers"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/pluginAPI"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/utils"
	"github.com/gorilla/mux"
)

var enforceQuota = os.Getenv("SWARM_ENFORCE_QUOTA")
var quotaMgmt QuotaMgmt
var initQuota = false //initialize quota once

type DefaultQuotaImpl struct {
	nextHandler pluginAPI.Handler
}

func NewQuota(handler pluginAPI.Handler) pluginAPI.PluginAPI {
	log.Debug("Plugin Quota NewQuota")
	quotaPlugin := &DefaultQuotaImpl{
		nextHandler: handler,
	}
	return quotaPlugin
}

func (quotaImpl *DefaultQuotaImpl) Handle(command utils.CommandEnum, cluster cluster.Cluster, w http.ResponseWriter, r *http.Request, swarmHandler http.Handler) utils.ErrorInfo {
	var errInfo utils.ErrorInfo
	errInfo.Status = http.StatusBadRequest
	if enforceQuota != "true" {
		log.Debug("Quota NOT enforced!")
		swarmHandler.ServeHTTP(w, r)
		errInfo.Err = nil
		return errInfo
	}
	log.Debug("Plugin Quota got command: " + command)
	//initialize quota once
	if initQuota == false {
		quotaMgmt.Init(cluster)
		initQuota = true
	}

	switch command {
	case utils.CONTAINER_CREATE:
		defer r.Body.Close()
		if reqBody, _ := ioutil.ReadAll(r.Body); len(reqBody) > 0 {
			var oldconfig clusterParams.OldContainerConfig
			if err := json.NewDecoder(bytes.NewReader(reqBody)).Decode(&oldconfig); err != nil {
				errInfo.Err = err
				return errInfo
			}

			// make sure HostConfig fields are consolidated before creating container
			clusterParams.ConsolidateResourceFields(&oldconfig)
			config := oldconfig.ContainerConfig

			memory := config.HostConfig.Memory
			tenant := r.Header.Get(headers.AuthZTenantIdHeaderName)
			// Increase tenant quota usage if quota limit isn't exceeded.
			err := quotaMgmt.CheckAndIncreaseQuota(tenant, memory)
			if err != nil {
				log.Error(err)
				errInfo.Err = err
				return errInfo
			}
			r.Body = ioutil.NopCloser(bytes.NewBuffer(reqBody))
			rec := httptest.NewRecorder()

			swarmHandler.ServeHTTP(rec, r)
			/*POST Swarm*/
			w.WriteHeader(rec.Code)
			for k, v := range rec.Header() {
				w.Header()[k] = v
			}
			w.Write(rec.Body.Bytes())
			//only if create container succeeded - add container
			err = quotaMgmt.HandleCreateResponse(rec.Code, rec.Body.Bytes(), tenant, memory) //checks that createContainer succeeded
			if err != nil {
				log.Error(err)
				errInfo.Err = err
				return errInfo
			}
		}
	case utils.CONTAINER_DELETE:
		resourceLongID := mux.Vars(r)["name"]
		tenant := r.Header.Get(headers.AuthZTenantIdHeaderName)
		//on delete request - decrease resource usage for the tenant in quotaService and set quota container status to PENDING_DELETED
		quotaMgmt.DecreaseQuota(resourceLongID, tenant)
		rec := httptest.NewRecorder()
		swarmHandler.ServeHTTP(rec, r)
		//if delete response is OK delete the container
		quotaMgmt.HandleDeleteResponse(rec.Code, resourceLongID, tenant)

	default:
		swarmHandler.ServeHTTP(w, r)
	}
	errInfo.Err = nil
	return errInfo
}
