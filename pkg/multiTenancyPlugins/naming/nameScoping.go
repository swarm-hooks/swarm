package namescoping

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strings"
	"errors"
	
	apitypes "github.com/docker/engine-api/types"
	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm/cluster"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/headers"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/pluginAPI"
	"github.com/docker/swarm/pkg/multiTenancyPlugins/utils"
	"github.com/gorilla/mux"
	"github.com/samalba/dockerclient"
)

//AuthenticationImpl - implementation of plugin API
type DefaultNameScopingImpl struct {
	nextHandler pluginAPI.Handler
}

func NewNameScoping(handler pluginAPI.Handler) pluginAPI.PluginAPI {
	nameScoping := &DefaultNameScopingImpl{
		nextHandler: handler,
	}
	return nameScoping
}

func uniquelyIdentifyContainer(cluster cluster.Cluster, r *http.Request, w http.ResponseWriter) {
	resourceName := mux.Vars(r)["name"]
	tenantId := r.Header.Get(headers.AuthZTenantIdHeaderName)
Loop:
	for _, container := range cluster.Containers() {
		if container.Info.ID == resourceName {
			//Match by Full Id - Do nothing
			break
		} else {
			for _, name := range container.Names {
				if (resourceName == name || resourceName == container.Labels[headers.OriginalNameLabel]) && container.Labels[headers.TenancyLabel] == tenantId {
					//Match by Name - Replace to full ID
					mux.Vars(r)["name"] = container.Info.ID
					r.URL.Path = strings.Replace(r.URL.Path, resourceName, container.Info.ID, 1)
					break Loop
				}
			}
		}
		if strings.HasPrefix(container.Info.ID, resourceName) {
			mux.Vars(r)["name"] = container.Info.ID
			r.URL.Path = strings.Replace(r.URL.Path, resourceName, container.Info.ID, 1)
			break
		}
	}
}

//Handle authentication on request and call next plugin handler.
func (nameScoping *DefaultNameScopingImpl) Handle(command string, cluster cluster.Cluster, w http.ResponseWriter, r *http.Request, swarmHandler http.Handler) error {
	log.Debug("Plugin nameScoping Got command: " + command)
	switch command {
	case "containercreate":
		var newQuery string
		var buf bytes.Buffer
		var containerConfig dockerclient.ContainerConfig
		var reqBody []byte
		defer r.Body.Close()
		if reqBody, _ = ioutil.ReadAll(r.Body); len(reqBody) > 0 {
			if err := json.NewDecoder(bytes.NewReader(reqBody)).Decode(&containerConfig); err != nil {
				log.Error(err)
				return err
			}
		}
		if "" != r.URL.Query().Get("name") {
			log.Debug("Postfixing name with tenantID...")
			newQuery = strings.Replace(r.RequestURI, r.URL.Query().Get("name"), r.URL.Query().Get("name")+r.Header.Get(headers.AuthZTenantIdHeaderName), 1)
			containerConfig.Labels[headers.OriginalNameLabel] = r.URL.Query().Get("name")
		}
		if err := CheckContainerReferences(cluster,r.Header.Get(headers.AuthZTenantIdHeaderName),&containerConfig); err != nil {
			log.Error(err)
			return err
		}
		if len(reqBody) > 0 {
			if err := json.NewEncoder(&buf).Encode(containerConfig); err != nil {
				log.Error(err)
				return err
			}
			r, _ = utils.ModifyRequest(r, bytes.NewReader(buf.Bytes()), newQuery, "")
		}

		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)

	//Find the container and replace the name with ID
	case "containerjson":
		if resourceName := mux.Vars(r)["name"]; resourceName != "" {
			uniquelyIdentifyContainer(cluster, r, w)
			return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)
		}
	case "containerstart", "containerstop", "containerdelete", "containerkill", "containerpause", "containerunpause", "containerupdate", "containercopy", "containerattach", "containerlogs":
		uniquelyIdentifyContainer(cluster, r, w)
		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)
	case "createNetwork":		
		defer r.Body.Close()
		if reqBody, _ := ioutil.ReadAll(r.Body); len(reqBody) > 0 {
			
			var request apitypes.NetworkCreate
			if err := json.NewDecoder(bytes.NewReader(reqBody)).Decode(&request); err != nil {
         		log.Error(err)
         		return nil
			}
			request.Name = r.Header.Get(headers.AuthZTenantIdHeaderName) + request.Name
			var buf bytes.Buffer
			if err := json.NewEncoder(&buf).Encode(request); err != nil {
				log.Error(err)
				return nil
			}
			r, _ = utils.ModifyRequest(r, bytes.NewReader(buf.Bytes()), "", "")
		}	
		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)	
	case "listContainers", "listNetworks", "clusterInfo":
		return nameScoping.nextHandler(command, cluster, w, r, swarmHandler)
	default:

	}
	return nil
}
func CheckContainerReferences(cluster cluster.Cluster, tenantName string, containerConfig *dockerclient.ContainerConfig) (error) {
	log.Debug("in CheckContainerReferences")
	log.Debugf("containerConfig: %+v",containerConfig)
	linksSize := len(containerConfig.HostConfig.Links)
	volumesFrom := containerConfig.HostConfig.VolumesFrom
	volumesFromSize := len(containerConfig.HostConfig.VolumesFrom)
	env := containerConfig.Env
	containers := cluster.Containers()
	linkSet := make(map[string]bool)
	links := make([]string,0)
	var v int  // count of volumesFrom links validated 
	var l int  // count of links validated
	var affinityContainerRef string  // docker affinity container env var ( affinity:container==<container ref> ) 
	var affinityContainerIndex int   // index of affinity container env element in env[]
	var affinityLabelRef string  // docker affinity label env var ( affinity:<label>==<label value> ) 
	var affinityLabelIndex int   // index of affinity label env element in env[]
	// check for affinity in environment vars.  
	// If yes save it for checking whether it belongs to tenant.
	affinityContainerCheckRequired := false
	affinityLabelCheckRequired := false
	for i, e := range env {
		if strings.HasPrefix(e,"affinity:") {
		  if strings.HasPrefix(e,"affinity:image==") {
			break  // we ignore affinity for images 
		  } else if strings.HasPrefix(e,"affinity:container==") {
			affinityContainerCheckRequired = true
			containerRefIndex := strings.Index(e,"affinity:container==")+len("affinity:container==")
			affinityContainerRef = e[containerRefIndex:len(e)]
			affinityContainerIndex = i
			break
		  } else { // affinity:<label>:<value>
			affinityLabelCheckRequired = true
			labelRefIndex := strings.Index(e,"affinity:")+len("affinity:")
			affinityLabelRef = e[labelRefIndex:len(e)]
			log.Debug("affinityLabelRef: ",affinityLabelRef)
			affinityLabelIndex = i
			break
		  }
		}
	}
	// if nothing to do return.
	if linksSize < 1 && volumesFromSize < 1 && !affinityContainerCheckRequired && !affinityLabelCheckRequired {
		return nil
	}
	// Cycle through all the containers.  
	// When a container is owned by tenant check whether it is referenced by current request.
	// If yes modify request with associated container id
	for _, container := range containers {
		//log.Debugf("Examine container %s %s",container.Info.Name,container.Info.ID)
		if(container.Config.Labels[headers.TenancyLabel] == tenantName) {
			log.Debugf("Look for container references in container %s %s for tenant %s",container.Info.Name,container.Info.ID,tenantName)
			// check for volumeFrom reference
			for i := 0; i < volumesFromSize; i++ {
				if v == volumesFromSize {
					break
				}
				log.Debugf("Examine VolumeFrom[%d] == %s", i, containerConfig.HostConfig.VolumesFrom[i])
				// volumesFrom element format <container_name>:<RW|RO>
				volumeFromArray := strings.SplitN(strings.TrimSpace(containerConfig.HostConfig.VolumesFrom[i]),":",2)
				volumeFrom := strings.TrimSpace(volumeFromArray[0])				
				if strings.HasPrefix(container.Info.ID,volumeFrom) {
					log.Debug("volumesFrom element with container id matches tenant container")
					// no need to modify volumesFrom
					v++					
				} else if container.Info.Name == "/"+volumeFrom+tenantName {
					log.Debug("volumesFrom element with container name matches tenant container")
					volumesFrom[i] = container.Info.ID
					if len(volumeFromArray) > 1 {
						volumesFrom[i] += ":"
						volumesFrom[i] += strings.TrimSpace(volumeFromArray[1])
					}
					v++					
				}
			}
			// check for links reference
			for i := 0; i < linksSize; i++ {
				if l == linksSize {
						break
				}
				log.Debugf("Examine links[%d] == %s", i, containerConfig.HostConfig.Links[i])

				linkArray := strings.SplitN(containerConfig.HostConfig.Links[i],":",2)
				link := strings.TrimSpace(linkArray[0])
				if strings.HasPrefix(container.Info.ID,link) || "/"+link+tenantName == container.Info.Name {
					log.Debug("Add link and alias to linkset")
					_, ok := linkSet[link]
					if !ok {
						linkSet[link] = true
						links = append(links,container.Info.ID + ":" + link)						
					}
					// check for alias  
					if len(linkArray) > 1 {						
						links = append(links,container.Info.ID + ":" + strings.TrimSpace(linkArray[1]))
					}
					l++
				}
			}
			// check for affinity:container==<container> reference
			// modify affinity container environment variable to reference container+tenantName
			if affinityContainerCheckRequired {
				if strings.HasPrefix(container.Info.ID,affinityContainerRef) {
				  affinityContainerCheckRequired = false				     
				} else if container.Info.Name == "/"+affinityContainerRef+tenantName {
				  env[affinityContainerIndex] = "affinity:container==" + container.Info.ID
				  log.Debugf("Updated environment variable %s ",env[affinityContainerIndex])
				  affinityContainerCheckRequired = false	  
				}
			}
			// check for affinity:<label key>==<label value> reference
			// modify affinity label container environment variable with affinity container env var to reference container id
			if affinityLabelCheckRequired {
				kv := strings.Split(affinityLabelRef,"==")
				for k,v := range container.Config.Labels {
					if k == kv[0] && v == kv[1] {
						affinityLabelCheckRequired=false
						env[affinityLabelIndex] = "affinity:container==" + container.Info.ID
						log.Debugf("Updated environment variable %s ",env[affinityLabelIndex])
						break						
					} 
				}
			}

		}
		// are we done?
		if v == volumesFromSize && l == linksSize && !affinityContainerCheckRequired && !affinityLabelCheckRequired {
			break
		}
	}
	// if references have not be satisfied return error
	if v != volumesFromSize || l != linksSize || affinityContainerCheckRequired || affinityLabelCheckRequired {
		return errors.New("Tenant not authorized to references containers that it does not own.")
	}
	containerConfig.HostConfig.VolumesFrom = volumesFrom
	containerConfig.HostConfig.Links = links
	containerConfig.Env = env
	return nil

}
