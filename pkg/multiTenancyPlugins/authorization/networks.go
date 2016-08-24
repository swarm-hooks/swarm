package authorization

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	log "github.com/Sirupsen/logrus"
	apitypes "github.com/docker/engine-api/types"
	"github.com/docker/swarm/cluster"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/headers"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/utils"
	"github.com/gorilla/mux"
	"io/ioutil"
	"net/http"
	"os"
)

func ConnectDisconnect(cluster cluster.Cluster, r *http.Request) utils.ErrorInfo {
	var errInfo utils.ErrorInfo
	errInfo.Status = http.StatusBadRequest
	if !utils.IsResourceOwner(cluster, r.Header.Get(headers.AuthZTenantIdHeaderName), mux.Vars(r)["networkid"], "network") {
		errInfo.Err = errors.New(fmt.Sprintf("No such network: %s", mux.Vars(r)["networkid"]))
		errInfo.Status = http.StatusNotFound
		return errInfo
	}
	defer r.Body.Close()
	if reqBody, _ := ioutil.ReadAll(r.Body); len(reqBody) > 0 {
		var request apitypes.NetworkConnect
		if err := json.NewDecoder(bytes.NewReader(reqBody)).Decode(&request); err != nil {
			errInfo.Err = err
			return errInfo
		}
		if !utils.IsOwnedByTenant(r.Header.Get(headers.AuthZTenantIdHeaderName), request.Container) {
			errInfo.Err = errors.New(fmt.Sprint("status ", http.StatusNotFound, " HTTP error: No such container"))
			errInfo.Status = http.StatusNotFound
			return errInfo
		}
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(request); err != nil {
			errInfo.Err = err
			return errInfo
		}
		r, _ = utils.ModifyRequest(r, bytes.NewReader(buf.Bytes()), "", "")
	}
	errInfo.Err = nil
	return errInfo
}

func NetworkAuthorization(cluster cluster.Cluster, r *http.Request, network string) error {
	// remove this when networks will be created by Swarm only
	if os.Getenv("SWARM_NETWORK_AUTHORIZATION") == "false" {
		log.Debug("Network authorization is turned off.")
		return nil
	}
	// allow Docker default networks.
	if network == "default" || network == "bridge" || network == "host" || network == "none" {
		return nil
	}
	if !utils.IsResourceOwner(cluster, r.Header.Get(headers.AuthZTenantIdHeaderName), network, "network") {
		return errors.New(fmt.Sprintf("No such network: %s", mux.Vars(r)["networkid"]))
	}
	return nil
}
