// volumes
package namescoping

import (
	"errors"
	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/headers"
	"github.com/gorilla/mux"
	"net/http"
	"strings"
)

const VOLUME_NAME_REQUIRED = "Volume name required."

func getVolumeBindings(r *http.Request, volumeBindings []string) []string {
	for i, volumeBinding := range volumeBindings {
		// volume binding spec should be in the format [source:]destination[:mode]
		arr := strings.SplitN(volumeBinding, ":", 3)
		if len(arr) > 1 {
			volumeBindings[i] = strings.Replace(volumeBindings[i], arr[0], arr[0]+r.Header.Get(headers.AuthZTenantIdHeaderName), 1)
		}
	}
	return volumeBindings

}

func nameScopeVolumeName(r *http.Request, volumeName string) (string, error) {
	if volumeName == "" {
		log.Debug(VOLUME_NAME_REQUIRED)
		return "", errors.New(VOLUME_NAME_REQUIRED)
	}
	return volumeName + r.Header.Get(headers.AuthZTenantIdHeaderName), nil
}

// append tenantid to volume name
func updateVolumeName(r *http.Request, muxVolumeName string) {
	volumeName := mux.Vars(r)[muxVolumeName] + r.Header.Get(headers.AuthZTenantIdHeaderName)
	r.URL.Path = strings.Replace(r.URL.Path, mux.Vars(r)[muxVolumeName], volumeName, 1)
	mux.Vars(r)[muxVolumeName] = volumeName
	return
}
