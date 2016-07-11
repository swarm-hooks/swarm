package authorization

import (
	"bytes"
	"encoding/json"
	"errors"
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

func ConnectDisconnect(cluster cluster.Cluster, r *http.Request) error {
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
			return errors.New("Not Authorized or no such container!")
		}
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(request); err != nil {
			return err
		}
		r, _ = utils.ModifyRequest(r, bytes.NewReader(buf.Bytes()), "", "")
	}
	return nil
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
		return errors.New("Not authorized or no such network!")
	}
	return nil
}
