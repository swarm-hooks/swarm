package filter

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm/cluster"
	"github.com/docker/swarm/scheduler/node"
	"os"
	"strconv"
)

// ResourceFilter selects only nodes based on other containers on the node.
type ResourceFilter struct {
}

// Name returns the name of the filter
func (f *ResourceFilter) Name() string {
	return "resource"
}

// Filter is exported
func (f *ResourceFilter) Filter(config *cluster.ContainerConfig, nodes []*node.Node, _ bool) ([]*node.Node, error) {
	availableMemory, err := strconv.ParseInt(os.Getenv("SWARM_NODE_MEMORY_LIMIT_MB"), 10, 64)
	if err != nil {
		log.Warning("Swarm node memory limit was not set.")
		availableMemory = -1
	} else {
		availableMemory = availableMemory * 1024 * 1024
	}
	availableCpu, err := strconv.ParseInt(os.Getenv("SWARM_NODE_CPU_LIMIT"), 10, 64)
	if err != nil {
		log.Warning("Swarm node CPU limit was not set.")
		availableCpu = -1
	}
	containerMemory := config.HostConfig.Memory
	containerCpu := config.HostConfig.CPUShares
	candidates := []*node.Node{}
	var memoryLimit int64
	var cpuLimit int64
	for _, node := range nodes {
		if availableMemory < 0 || availableMemory > node.TotalMemory {
			memoryLimit = node.TotalMemory
		} else {
			memoryLimit = availableMemory
		}
		if availableCpu < 0 || availableCpu > node.TotalCpus {
			cpuLimit = node.TotalCpus
		} else {
			cpuLimit = availableCpu
		}
		if node.UsedMemory+containerMemory <= memoryLimit && node.UsedCpus+containerCpu <= cpuLimit {
			candidates = append(candidates, node)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("No resources available to place container.")
	}
	return candidates, nil
}

// GetFilters returns resources info.
func (f *ResourceFilter) GetFilters(config *cluster.ContainerConfig) ([]string, error) {
	return []string{fmt.Sprintf("Memory=%d CPU=%d", config.HostConfig.Memory, config.HostConfig.CPUShares)}, nil
}
