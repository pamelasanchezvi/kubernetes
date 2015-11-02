package vmt

import (
	"fmt"
	"time"

	cadvisorClient "github.com/google/cadvisor/client"
	cadvisor "github.com/google/cadvisor/info/v1"

	"github.com/golang/glog"
)

type cadvisorSource struct{}

// Construct a container from containerInfo to the Container type defined in types.
func (self *cadvisorSource) parseStat(containerInfo *cadvisor.ContainerInfo) *Container {
	container := &Container{
		Name:  containerInfo.Name,
		Spec:  ContainerSpec{ContainerSpec: containerInfo.Spec},
		Stats: sampleContainerStats(containerInfo.Stats),
	}
	if len(containerInfo.Aliases) > 0 {
		container.Name = containerInfo.Aliases[0]
	}

	return container
}

// Get all containers from cAdvisor and separates the root container and other contianers.
func (self *cadvisorSource) getAllContainers(client *cadvisorClient.Client, start, end time.Time) (subcontainers []*Container, root *Container, err error) {
	allContainers, err := client.SubcontainersInfo("/",
		&cadvisor.ContainerInfoRequest{})
	if err != nil {
		glog.Errorf("Got error when trying to get container info: %v", err)
		return nil, nil, err
	}

	for _, containerInfo := range allContainers {
		container := self.parseStat(&containerInfo)
		if containerInfo.Name == "/" {
			root = container
		} else {
			subcontainers = append(subcontainers, container)
		}
	}

	return subcontainers, root, nil
}

// Get all the containers in specified host.
func (self *cadvisorSource) GetAllContainers(host Host, start, end time.Time) (subcontainers []*Container, root *Container, err error) {
	url := fmt.Sprintf("http://%s:%d/", host.IP, host.Port)
	client, err := cadvisorClient.NewClient(url)
	if err != nil {
		return
	}
	subcontainers, root, err = self.getAllContainers(client, start, end)
	if err != nil {
		glog.Errorf("failed to get stats from cadvisor %q - %v\n", url, err)
	}
	return
}

// Get node information from cAdvisor.
func (self *cadvisorSource) GetMachineInfo(host Host) (machineInfo *cadvisor.MachineInfo, err error) {
	url := fmt.Sprintf("http://%s:%d/", host.IP, host.Port)
	client, err := cadvisorClient.NewClient(url)
	if err != nil {
		glog.Errorf("Failed to create cAdvisor client: %s", err)
		return nil, fmt.Errorf("Failed to create cAdvisor client: %s", err)
	}
	machineInfo, err = client.MachineInfo()
	if err != nil {
		glog.Errorf("failed to get stats from cadvisor %q - %v\n", url, err)
		return nil, fmt.Errorf("failed to get stats from cadvisor %q - %v\n", url, err)
	}
	return
}

// Return a list of ContainerStats.
func sampleContainerStats(stats []*cadvisor.ContainerStats) []*ContainerStats {
	if len(stats) == 0 {
		return []*ContainerStats{}
	}

	var res []*ContainerStats
	for _, stat := range stats {
		res = append(res, &ContainerStats{ContainerStats: *stat})
	}
	return res
}
