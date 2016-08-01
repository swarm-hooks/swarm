package authentication

import (
	"errors"
	"net/http"
	"os"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm/cluster"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/headers"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/pluginAPI"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/utils"
)

//AuthenticationImpl - implementation of plugin API
type AuthenticationImpl struct {
	nextHandler pluginAPI.Handler
}

func NewAuthentication(handler pluginAPI.Handler) pluginAPI.PluginAPI {
	authN := &AuthenticationImpl{
		nextHandler: handler,
	}
	return authN
}

//Handle authentication on request and call next plugin handler.
func (authentication *AuthenticationImpl) Handle(command utils.CommandEnum, cluster cluster.Cluster, w http.ResponseWriter, r *http.Request, swarmHandler http.Handler) utils.ErrorInfo {
	log.Debug("Plugin authN got command: " + command)
	var errInfo utils.ErrorInfo
	errInfo.Status = http.StatusBadRequest
	tenantIdToValidate := r.Header.Get(headers.AuthZTenantIdHeaderName)
	if tenantIdToValidate == "" {
		errInfo.Err = errors.New("Not Authorized!")
		return errInfo
	}
	if tenantIdToValidate == os.Getenv("SWARM_ADMIN_TENANT_ID") {
		swarmHandler.ServeHTTP(w, r)
		errInfo.Err = nil
		return errInfo
	}
	return authentication.nextHandler(command, cluster, w, r, swarmHandler)
}
