package dataInit

import (
	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm/cluster"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/headers"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/pluginAPI"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/utils"
	"github.com/gorilla/mux"
	"net/http"
	"net/http/httptest"
)

//MappingImpl - implementation of plugin API
type MappingImpl struct {
	nextHandler pluginAPI.Handler
}

func NewMapping(handler pluginAPI.Handler) pluginAPI.PluginAPI {
	log.Debug("Initializing container maps")
	utils.FullIdToTenant = make(map[string]string)
	utils.ContainerFullIds =  make(map[string]*cluster.Container)
	utils.ContainerShortIds =  make(map[string]*cluster.Container)
	utils.ContainerNames =  make(map[string]*cluster.Container)
	containerMapping := &MappingImpl{
		nextHandler: handler,
	}
	return containerMapping
}

var initialized bool

//Handle container mapping on request and call next plugin handler.
func (mapping *MappingImpl) Handle(command utils.CommandEnum, cluster cluster.Cluster, w http.ResponseWriter, r *http.Request, swarmHandler http.Handler) utils.ErrorInfo {
	log.Debug("Plugin mapping got command: " + command)
	if !initialized {
		go utils.InitialMapping(cluster)
		initialized = true
	}
	var errInfo utils.ErrorInfo
	switch command {
	case utils.CONTAINER_CREATE:
		rec := httptest.NewRecorder()
		if errInfo := mapping.nextHandler(command, cluster, rec, r, swarmHandler); errInfo.Err != nil {
			return errInfo
		}
		w.WriteHeader(rec.Code)
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.Write(rec.Body.Bytes())
		utils.CreateContainerMapping(rec.Code, rec.Body.Bytes(), r.Header.Get(headers.AuthZTenantIdHeaderName), cluster)

	case utils.CONTAINER_DELETE:
		var containerID string
		var originalName string
		var containerNames []string
		var tenantID string
		container := utils.GetContainer(mux.Vars(r)["name"], r.Header.Get(headers.AuthZTenantIdHeaderName), cluster)
		if container != nil {
			containerID = container.Info.ID
			containerNames = container.Names
			tenantID = r.Header.Get(headers.AuthZTenantIdHeaderName)
			originalName = container.Labels[headers.OriginalNameLabel]
		}		
		rec := httptest.NewRecorder()
		if errInfo := mapping.nextHandler(command, cluster, rec, r, swarmHandler); errInfo.Err != nil {
			return errInfo
		}
		w.WriteHeader(rec.Code)
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.Write(rec.Body.Bytes())
		utils.DeleteContainerMapping(rec.Code, containerID, containerNames, tenantID, originalName)
		
	default:
		return mapping.nextHandler(command, cluster, w, r, swarmHandler)
	}
	errInfo.Err = nil
	return errInfo
}
