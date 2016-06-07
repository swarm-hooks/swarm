package multiTenancyPlugins

import (
	"net/http"
	"os"
	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm/cluster"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/quota"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/authentication"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/authorization"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/flavors"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/naming"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/pluginAPI"
)

//Executor - Entry point to multi-tenancy plugins
type Executor struct{}

var startHandler pluginAPI.Handler

//Handle - Hook point from primary to plugins
func (*Executor) Handle(cluster cluster.Cluster, swarmHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if os.Getenv("SWARM_MULTI_TENANT") == "false" {
			swarmHandler.ServeHTTP(w, r)
			return
		}
		err := startHandler(utils.ParseCommand(r), cluster, w, r, swarmHandler)
		if err != nil {
			log.Error(err)
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	})
}

//Init - Initialize the Validation and Handling plugins
func (*Executor) Init() {
	if os.Getenv("SWARM_MULTI_TENANT") == "false" {
		log.Debug("SWARM_MULTI_TENANT is false")
		return
	}
	if os.Getenv("SWARM_AUTH_BACKEND") == "Keystone" {
		log.Debug("Keystone not supported")
	} else {
		quotaPlugin := quota.NewQuota(nil)
		authorizationPlugin := authorization.NewAuthorization(quotaPlugin.Handle)
		nameScoping := namescoping.NewNameScoping(authorizationPlugin.Handle)
		flavorsPlugin := flavors.NewPlugin(nameScoping.Handle)
		//authenticationPlugin := authentication.NewAuthentication(nameScoping.Handle)
		authenticationPlugin := authentication.NewAuthentication(flavorsPlugin.Handle)
		startHandler = authenticationPlugin.Handle
	}
}
