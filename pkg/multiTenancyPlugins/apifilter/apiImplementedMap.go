package apifilter

import (
	"encoding/json"
	log "github.com/Sirupsen/logrus"
	c "github.com/docker/swarm/pkg/multiTenancyPlugins/utils"
	"os"
)

var supportedAPIsMap map[c.CommandEnum]bool

func initSupportedAPIsMap() {
	supportedAPIsMap = make(map[c.CommandEnum]bool)
	//containers
	supportedAPIsMap[c.CONTAINER_CREATE] = true
	supportedAPIsMap[c.CONTAINER_JSON] = true
	supportedAPIsMap[c.PS] = true
	//container
	supportedAPIsMap[c.CONTAINER_START] = true
	supportedAPIsMap[c.CONTAINER_ARCHIVE] = true
	supportedAPIsMap[c.CONTAINER_ATTACH] = true
	supportedAPIsMap["containerbuild"] = true
	supportedAPIsMap[c.CONTAINER_COPY] = true
	supportedAPIsMap[c.CONTAINER_CHANGES] = true
	supportedAPIsMap[c.EVENTS] = true
	supportedAPIsMap[c.CONTAINER_EXEC] = true
	supportedAPIsMap[c.EXEC_START] = true  //exec/{execid:.*}/start
	supportedAPIsMap[c.EXEC_RESIZE] = true //exec/{execid:.*}/resize
	supportedAPIsMap[c.EXEC_JSON] = true   //exec/{execid:.*}/json
	supportedAPIsMap[c.CONTAINER_EXPORT] = true
	supportedAPIsMap[c.CONTAINER_IMPORT] = true
	supportedAPIsMap[c.CONTAINER_JSON] = true
	supportedAPIsMap[c.CONTAINER_RESTART] = true
	supportedAPIsMap[c.CONTAINER_KILL] = true
	supportedAPIsMap[c.CONTAINER_LOGS] = true
	supportedAPIsMap[c.CONTAINER_PAUSE] = true
	supportedAPIsMap["containertport"] = true
	supportedAPIsMap[c.CONTAINER_RENAME] = false
	supportedAPIsMap[c.CONTAINER_DELETE] = true
	supportedAPIsMap[c.CONTAINER_STOP] = true
	supportedAPIsMap[c.CONTAINER_TOP] = true
	supportedAPIsMap[c.CONTAINER_UNPAUSE] = true
	supportedAPIsMap[c.CONTAINER_UPDATE] = true
	supportedAPIsMap[c.CONTAINER_WAIT] = true
	supportedAPIsMap[c.JSON] = true
	supportedAPIsMap[c.CONTAINER_STATS] = true
	supportedAPIsMap[c.CONTAINER_RESIZE] = false
	//image
	supportedAPIsMap["imagecommit"] = false
	supportedAPIsMap[c.IMAGE_HISTORY] = true
	supportedAPIsMap["imageimport"] = false
	supportedAPIsMap["imageload"] = false
	supportedAPIsMap[c.IMAGE_PULL] = true
	supportedAPIsMap["imagepush"] = false
	supportedAPIsMap["imagedelete"] = true
	supportedAPIsMap["imagesave"] = false
	supportedAPIsMap[c.IMAGE_SEARCH] = true
	supportedAPIsMap[c.IMAGE_JSON] = true //inspect image
	supportedAPIsMap["imagetag"] = false
	supportedAPIsMap[c.IMAGES_JSON] = true //listImages
	//server
	supportedAPIsMap["serverlogin"] = false
	supportedAPIsMap["serverlogout"] = false
	//Network
	supportedAPIsMap["networkslist"] = true
	supportedAPIsMap["networkinspect"] = true
	supportedAPIsMap["networkconnect"] = true
	supportedAPIsMap["networkdisconnect"] = true
	supportedAPIsMap["networkcreate"] = true
	supportedAPIsMap["networkdelete"] = true
	//Volume
	supportedAPIsMap[c.VOLUME_CREATE] = true
	supportedAPIsMap[c.VOLUME_INSPECT] = true
	supportedAPIsMap[c.VOLUMES_LIST] = true
	supportedAPIsMap[c.VOLUME_DELETE] = true

	//general
	supportedAPIsMap[c.INFO] = true
	supportedAPIsMap[c.VERSION] = true

	//new
	supportedAPIsMap["ping"] = false                  //_ping
	supportedAPIsMap["imagesviz"] = false             //notImplementedHandler
	supportedAPIsMap["getRepositoriesImages"] = false //images/get	(Get a tarball containing all images)
	supportedAPIsMap["getRepositoryImages"] = false   //images/{name:.*}/get	(Get a tarball containing all images in a repository)
	supportedAPIsMap["auth"] = false                  //auth
	supportedAPIsMap["commit"] = false                //commit
	supportedAPIsMap["build"] = false                 //build
	supportedAPIsMap[c.CONTAINER_RESIZE] = false      //containers/{name:.*}/resize
	//images/create:                    (Create an image) is it equal to imagepull??
}

func modifySupportedWithDisabledApi() {
	type Filter struct {
		Disableapi []c.CommandEnum
	}
	var filter Filter
	var f = os.Getenv("SWARM_APIFILTER_FILE")
	if f == "" {
		f = "apifilter.json"
	}

	file, err := os.Open(f)
	if err != nil {
		log.Info("no API FILTER file")
		return
	}

	log.Info("SWARM_APIFILTER_FILE: ", f)

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&filter)
	if err != nil {
		log.Fatal("Error in apifilter decode:", err)
		panic("Error: could not decode apifilter.json")
	}
	log.Infof("filter %+v", filter)
	for _, e := range filter.Disableapi {
		if supportedAPIsMap[e] {
			log.Infof("disable %+v", e)
			supportedAPIsMap[e] = false
		}

	}
}
