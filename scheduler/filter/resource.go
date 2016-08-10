package filter

import (
	"fmt"
	"github.com/docker/swarm/cluster"
	"github.com/docker/swarm/scheduler/node"
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
	containerMemory := config.HostConfig.Memory
	containerCpu := config.HostConfig.CPUShares
	candidates := []*node.Node{}
	for _, node := range nodes {
		availableMemory := node.TotalMemory * 50 / 100
		availableCpu := node.TotalCpus * 50 / 100
		if node.UsedMemory+containerMemory <= availableMemory && node.UsedCpus+containerCpu <= availableCpu {
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
