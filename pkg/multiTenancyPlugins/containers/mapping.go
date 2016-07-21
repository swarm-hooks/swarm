package containers

import (
	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm/cluster"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/headers"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/pluginAPI"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/utils"
	"github.com/gorilla/mux"
	"net/http"
	"net/http/httptest"
	"strings"
)

//MappingImpl - implementation of plugin API
type MappingImpl struct {
	nextHandler pluginAPI.Handler
}

func NewMapping(handler pluginAPI.Handler) pluginAPI.PluginAPI {
	log.Debug("Initializing containers map")
	containers = make(map[string]string)
	containerMapping := &MappingImpl{
		nextHandler: handler,
	}
	return containerMapping
}

var containers map[string]string

//Handle container mapping on request and call next plugin handler.
func (mapping *MappingImpl) Handle(command utils.CommandEnum, cluster cluster.Cluster, w http.ResponseWriter, r *http.Request, swarmHandler http.Handler) error {
	log.Debug("Plugin mapping got command: " + command)

	switch command {
	case utils.CONTAINER_CREATE:
		containerName := r.URL.Query().Get("name")
		rec := httptest.NewRecorder()
		err := mapping.nextHandler(command, cluster, rec, r, swarmHandler)
		w.WriteHeader(rec.Code)
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.Write(rec.Body.Bytes())
		createContainerMapping(rec.Code, rec.Body.Bytes(), r.Header.Get(headers.AuthZTenantIdHeaderName), containerName)
		if err != nil {
			return err
		}

	case utils.CONTAINER_DELETE:
		container := getContainer(cluster, r)
		rec := httptest.NewRecorder()
		err := mapping.nextHandler(command, cluster, rec, r, swarmHandler)
		deleteContainerMapping(rec.Code, container)
		if err != nil {
			return err
		}
	default:
		return mapping.nextHandler(command, cluster, w, r, swarmHandler)
	}
	return nil
}

func getContainer(cluster cluster.Cluster, r *http.Request) *cluster.Container {
	containerName := mux.Vars(r)["name"]
	for _, container := range cluster.Containers() {
		if container.Info.ID == containerName && container.Labels[headers.TenancyLabel] == r.Header.Get(headers.AuthZTenantIdHeaderName) {
			//Match by Full Id
			return container
		} else {
			for _, name := range container.Names {
				if (containerName == strings.TrimPrefix(name, "/") || containerName == container.Labels[headers.OriginalNameLabel]) && container.Labels[headers.TenancyLabel] == r.Header.Get(headers.AuthZTenantIdHeaderName) {
					//Match by Name
					return container
				}
			}
		}
		if strings.HasPrefix(container.Info.ID, containerName) && container.Labels[headers.TenancyLabel] == r.Header.Get(headers.AuthZTenantIdHeaderName) {
			//Match by short ID
			return container
		}
	}
	return nil
}
