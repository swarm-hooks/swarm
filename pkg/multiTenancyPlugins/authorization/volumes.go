// volumes.go
package authorization

import (
	"bytes"
	"encoding/json"
	"errors"
	log "github.com/Sirupsen/logrus"
	apitypes "github.com/docker/engine-api/types"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/headers"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/utils"
	"github.com/gorilla/mux"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
)

const HOST_FS_MOUNT_NOT_ALLOWED = "Host file system mount not allowed!"
const VOLUME_REF_NOT_AUTHORIZED = "Volume reference Not Authorized or no such resource!"

func filterVolumes(r *http.Request, rec *httptest.ResponseRecorder) []byte {
	var response apitypes.VolumesListResponse
	if err := json.NewDecoder(bytes.NewReader(rec.Body.Bytes())).Decode(&response); err != nil {
		log.Error(err)
		return nil
	}
	var candidates []*apitypes.Volume
	for _, volume := range response.Volumes {
		if strings.HasSuffix(volume.Name, r.Header.Get(headers.AuthZTenantIdHeaderName)) {
			candidates = append(candidates, volume)
		}
	}
	response.Volumes = candidates
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(response); err != nil {
		log.Error(err)
		return nil
	}
	return buf.Bytes()
}
func hostFSMountCheck(tenantId string, volumeBindings []string) utils.ErrorInfo {
	var errorInfo utils.ErrorInfo
	for _, volumeBinding := range volumeBindings {
		// volume binding spec should be in the format [source:]destination[:mode]
		arr := strings.SplitN(volumeBinding, ":", 3)
		if len(arr) > 1 {
			if source := filepath.ToSlash(arr[0]); strings.Contains(source, "/") {
				errorInfo.Err = errors.New(HOST_FS_MOUNT_NOT_ALLOWED)
			}
		}
	}
	return errorInfo

}
func volumeOwnershipCheck(r *http.Request, muxVolumeName string) utils.ErrorInfo {
	var errorInfo utils.ErrorInfo
	if !strings.HasSuffix(mux.Vars(r)[muxVolumeName], r.Header.Get(headers.AuthZTenantIdHeaderName)) {
		errorInfo.Err = errors.New(VOLUME_REF_NOT_AUTHORIZED)
	}
	return errorInfo
}
