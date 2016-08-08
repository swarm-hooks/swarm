package namescoping

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
	"net/http/httptest"
	"strings"
)

func ConnectDisconnect(cluster cluster.Cluster, r *http.Request) error {
	if netName := mux.Vars(r)["networkid"]; netName != "" {
		setNetworkFullId(cluster, r, netName)
		defer r.Body.Close()
		// replace container name/shortID with caontainer full ID.
		if reqBody, _ := ioutil.ReadAll(r.Body); len(reqBody) > 0 {
			var request apitypes.NetworkConnect
			if err := json.NewDecoder(bytes.NewReader(reqBody)).Decode(&request); err != nil {
				return err
			}
			conatinerID, err := getContainerID(cluster, r.Header.Get(headers.AuthZTenantIdHeaderName), request.Container)
			if err != nil {
				log.Error(err)
				return errors.New(fmt.Sprint("No such container: ", request.Container))
			}
			request.Container = conatinerID
			var buf bytes.Buffer
			if err := json.NewEncoder(&buf).Encode(request); err != nil {
				return err
			}
			// set ContentLength for new  body
			r.ContentLength = int64(len(buf.Bytes()))
			r, _ = utils.ModifyRequest(r, bytes.NewReader(buf.Bytes()), "", "")
		}
	}
	return nil
}

func DeleteInspect(cluster cluster.Cluster, r *http.Request) {
	if netName := mux.Vars(r)["networkid"]; netName != "" {
		setNetworkFullId(cluster, r, netName)
	}
}

func CreateNetwork(cluster cluster.Cluster, r *http.Request) error {
	defer r.Body.Close()
	// prefix network name with tenant name.
	if reqBody, _ := ioutil.ReadAll(r.Body); len(reqBody) > 0 {
		var request apitypes.NetworkCreateRequest
		if err := json.NewDecoder(bytes.NewReader(reqBody)).Decode(&request); err != nil {
			return err
		}
		request.Name = r.Header.Get(headers.AuthZTenantIdHeaderName) + request.Name
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(request); err != nil {
			return err
		}
		r, _ = utils.ModifyRequest(r, bytes.NewReader(buf.Bytes()), "", "")
	}
	return nil
}

/*
   Replace network name/shortID with network full ID in http request.
*/
func setNetworkFullId(cluster cluster.Cluster, r *http.Request, netName string) {
	netID := getNetworkID(cluster, r, netName)
	r.URL.Path = strings.Replace(r.URL.Path, netName, netID, 1)
	mux.Vars(r)["networkid"] = netID
}

/*
   Return network full ID if network exists.
*/
func getNetworkID(cluster cluster.Cluster, r *http.Request, networkId string) string {
	tenantId := r.Header.Get(headers.AuthZTenantIdHeaderName)
	for _, network := range cluster.Networks() {
		if network.ID == networkId {
			//Match by Full ID.
			return network.ID
		} else {
			if network.Name == tenantId+networkId {
				//Match by name. Replace by full ID.
				return network.ID
			}
		}
		if strings.HasPrefix(network.ID, networkId) {
			//Match by short id. Replace by full ID.
			return network.ID
		}
	}
	return networkId
}

func cleanUpNames(responseRecorder *httptest.ResponseRecorder, networkName string) []byte {
	var networkResource apitypes.NetworkResource
	if err := json.NewDecoder(bytes.NewReader(responseRecorder.Body.Bytes())).Decode(&networkResource); err != nil {
		log.Error(err)
		return nil
	}
	networkResource.Name = networkName
	// TODO: change the names of the attached containers.
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(networkResource); err != nil {
		log.Error(err)
		return nil
	}
	return buf.Bytes()
}
