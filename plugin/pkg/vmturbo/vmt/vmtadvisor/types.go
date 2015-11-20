package vmtadvisor

import (
	"fmt"
	"time"

	"k8s.io/kubernetes/pkg/api"

	cadvisor "github.com/google/cadvisor/info/v1"
	info "github.com/google/cadvisor/info/v2"

	"github.com/golang/glog"
)

type Host struct {
	Name string
	IP   string
	Port int
	Spec *MachineSpec
}

type MachineSpec struct {
	CpuFrequency float64
	CpuCapacity  float64
	MemCapacity  float64
}

type MachineStats struct {
	CpuUsed float64
	MemUsed float64
}

// Get host information from node. The host is used to get all static or realtime information of the node from cAdvisor.
func NewCadvisorHost(node *api.Node) *Host {
	var nodeIP string
	nodeAddresses := node.Status.Addresses
	for _, nodeAddress := range nodeAddresses {
		// TODO Tests is needed if this is the correct address to be used.
		if nodeAddress.Type == api.NodeLegacyHostIP {
			nodeIP = nodeAddress.Address
		}
	}

	host := &Host{
		Name: node.Name,
		IP:   nodeIP,
		Port: 4194,
	}

	return host
}

//
func (host *Host) GetMachineSpec() (*MachineSpec, error) {
	if host.Spec != nil {
		return host.Spec, nil
	}
	err := host.setMachineSpec()
	return host.Spec, err
}

func (host *Host) setMachineSpec() error {
	machineSpec, err := host.parseMachineSpec()
	if err != nil {
		return err
	}
	host.Spec = machineSpec
	return nil
}

// Get machine specification of the current node. The spec includes Cpu frequency, Cpu capacity and Memory capacity.
func (host *Host) parseMachineSpec() (*MachineSpec, error) {
	// use cadvisor to get machine info
	cadvisor := &CadvisorSource{}
	machineInfo, err := cadvisor.GetMachineInfo(host)
	if err != nil {
		return nil, fmt.Errorf("Error getting machine info for %s: %s", host.Name, err)
	}
	// The cpu frequency return from cadvisor is in KHz, we need MHz
	cpuFrequency := float64(machineInfo.CpuFrequency) / 1000
	// Get the node Cpu and Mem capacity.
	nodeCpuCapacity := float64(machineInfo.NumCores) * float64(cpuFrequency)
	nodeMemCapacity := float64(machineInfo.MemoryCapacity) / 1024 // Mem is returned in B

	machineSpec := &MachineSpec{
		CpuFrequency: cpuFrequency,
		CpuCapacity:  nodeCpuCapacity,
		MemCapacity:  nodeMemCapacity,
	}

	return machineSpec, nil
}

// Get the realtime resource usage of the machine.The stats include Cpu usage and Mem usage.
func (host *Host) GetMachineCurrentStats() (*MachineStats, error) {

	cadvisor := &CadvisorSource{}

	// To get the machine level resource consumption, we only need the root container.
	_, root, err := cadvisor.GetAllContainers(host, time.Now(), time.Now())
	if err != nil {
		return nil, err
	}
	containerStats := root.Stats
	// To get a valid cpu usage, there must be at least 2 valid stats.
	if len(containerStats) < 2 {
		return nil, fmt.Errorf("Not enough status data of current node %s.", host.Name)
	}
	currentStat := containerStats[len(containerStats)-1]
	prevStat := containerStats[len(containerStats)-2]

	rawUsage := int64(currentStat.Cpu.Usage.Total - prevStat.Cpu.Usage.Total)
	glog.V(5).Infof("Raw Cpu usage of %s now is %d", host.Name, rawUsage)
	intervalInNs := currentStat.Timestamp.Sub(prevStat.Timestamp).Nanoseconds()
	glog.V(5).Infof("Interval is %d", intervalInNs)

	rootCpuUsed := float64(rawUsage) / float64(intervalInNs)

	machineSpec, err := host.GetMachineSpec()
	if err != nil {
		return nil, err
	}
	cpuUsed := rootCpuUsed * machineSpec.CpuFrequency
	memUsed := float64(currentStat.Memory.Usage) / 1024 // Mem is returned in B

	return &MachineStats{
		CpuUsed: cpuUsed,
		MemUsed: memUsed,
	}, nil
}

type PodInfo struct {
	ID             string
	HostingMachine *Host
	Containers     []*Container
	Spec           *PodSpec
}

type PodSpec struct {
	CpuCapacity float64
	MemCapacity float64
}

type PodStats struct {
	CpuUsed float64
	MemUsed float64
}

// Create a new PodInfo. To do so we must know the podID (which now is the "[namespace]/[name]")
// and the hosting machine infomation.
func NewPodInfo(podID string, host *Host) *PodInfo {
	return &PodInfo{
		ID:             podID,
		HostingMachine: host,
	}
}

// Get the pod specification.
func (podInfo *PodInfo) GetPodSpec(pod *api.Pod) (*PodSpec, error) {
	if podInfo.Spec != nil {
		return podInfo.Spec, nil
	}

	err := podInfo.setPodSpec(pod)
	return podInfo.Spec, err
}

// set the pod specificaiton.
func (podInfo *PodInfo) setPodSpec(pod *api.Pod) error {
	podSpec, err := podInfo.parsePodSpec(pod)
	if err != nil {
		return err
	}
	podInfo.Spec = podSpec
	return nil
}

// Retreive pod specification from container requests, including cpu/mem capacity.
func (podInfo *PodInfo) parsePodSpec(pod *api.Pod) (*PodSpec, error) {
	aggrContainerCpuRequest := int64(0)
	aggrContainerMemRequest := int64(0)

	// TODO! Here we assume when user defines a pod, resource requirements are also specified.
	// The metrics we care about now are Cpu and Mem.
	for _, container := range pod.Spec.Containers {
		requests := container.Resources.Limits
		aggrContainerMemRequest += requests.Memory().Value()
		aggrContainerCpuRequest += requests.Cpu().MilliValue()
	}

	// get cpu frequency and convert KHz to MHz
	hostSpec, err := podInfo.HostingMachine.GetMachineSpec()
	if err != nil {
		return nil, fmt.Errorf("Error getting hostSpec when parsing podSpec: %s", err)
	}
	cpuFrequency := hostSpec.CpuFrequency

	// the cpu return value is in core*1000, so here should divide 1000
	podCpuCapacity := float64(aggrContainerCpuRequest) / 1000 * cpuFrequency
	podMemCapacity := float64(aggrContainerMemRequest) / 1024 // Mem is in bytes, convert to Kb
	glog.V(4).Infof("Cpu cap of Pod %s is %f", podInfo.ID, podCpuCapacity)
	glog.V(4).Infof("Mem cap of Pod %s is %f", podInfo.ID, podMemCapacity)

	return &PodSpec{
		CpuCapacity: podCpuCapacity,
		MemCapacity: podMemCapacity,
	}, nil
}

// Get the current pod stats, including cpu/mem usage.
func (podInfo *PodInfo) GetPodCurrentStats() (*PodStats, error) {
	podCpuUsed := float64(0)
	podMemUsed := float64(0)

	containers := podInfo.Containers

	if containers == nil || len(containers) < 1 {
		// For debugging.
		glog.Errorf("There should have been container inside pod %s. Something wrong during preparsing.", podInfo.ID)
	}

	for _, container := range containers {
		containerStats := container.Stats
		// In order to get the cpu usage, we need at least two container stats.
		if len(containerStats) < 2 {
			// TODO, do we still continue probing the container if there is one container does not have enough data?
			// If we do, then we ignore current container.
			glog.Warningf("Not enough realtime usage data for container %s inside pod %s", container.Name, podInfo.ID)
			continue
		}
		currentStat := containerStats[len(containerStats)-1]
		prevStat := containerStats[len(containerStats)-2]
		rawUsage := int64(currentStat.Cpu.Usage.Total - prevStat.Cpu.Usage.Total)
		intervalInNs := currentStat.Timestamp.Sub(prevStat.Timestamp).Nanoseconds()
		podCpuUsed += float64(rawUsage) * 1.0 / float64(intervalInNs)
		podMemUsed += float64(currentStat.Memory.Usage)
	}

	// get cpu frequency and convert KHz to MHz
	hostSpec, err := podInfo.HostingMachine.GetMachineSpec()
	if err != nil {
		// This may not need.
		return nil, fmt.Errorf("Error getting hostSpec when getting pod realtime resource usage: %s", err)
	}
	cpuFrequency := hostSpec.CpuFrequency

	// convert num of core to frequecy in MHz
	podCpuUsed = podCpuUsed * cpuFrequency
	podMemUsed = podMemUsed / 1024 // Mem is in bytes, convert to Kb

	glog.V(3).Infof(" Discovered pod is " + podInfo.ID)
	glog.V(4).Infof(" Pod %s CPU request is %f", podInfo.ID, podCpuUsed)
	glog.V(4).Infof(" Pod %s Mem request is %f", podInfo.ID, podMemUsed)

	return &PodStats{
		CpuUsed: podCpuUsed,
		MemUsed: podMemUsed,
	}, nil
}

type Container struct {
	Hostname     string
	ExternalID   string
	Name         string
	Aliases      []string
	Image        string
	HostingPod   *PodInfo
	Applications map[string]*Application
	Spec         ContainerSpec
	Stats        []*ContainerStats
}

type ContainerSpec struct {
	cadvisor.ContainerSpec
	CpuRequest    int64
	MemoryRequest int64
}

type ContainerStats struct {
	cadvisor.ContainerStats
}

func (ctn *Container) AddApplication(app *Application) {
	if ctn.Applications == nil {
		ctn.Applications = make(map[string]*Application)
	}
	apps := ctn.Applications
	apps[app.Name] = app
	ctn.Applications = apps
}

type Application struct {
	Name             string
	ProcessName      string
	Processes        []info.ProcessInfo
	HostingContainer *Container
}

type ApplicationSpec struct {
	TransactionCapacity float64
}

type ApplicationStats struct {
	TransactionUsed float64
	CpuUsed         float64
	MemUsed         float64
}

func NewApplication(appName, processName string, container *Container) *Application {
	var processes []info.ProcessInfo
	return &Application{
		Name:             appName,
		ProcessName:      processName,
		Processes:        processes,
		HostingContainer: container,
	}
}

func (app *Application) GetApplicationSpec() (*ApplicationSpec, error) {
	return &ApplicationSpec{
		TransactionCapacity: float64(10000),
	}, nil
}

func (app *Application) GetApplicationCurrentStats() (*ApplicationStats, error) {
	percentCpu := float64(0)
	percentMem := float64(0)
	// TODO. Now we dont have a good way to get transaction data. Hard code to 9999(with capacity 10000) for test purpose.
	transactionUsed := float64(9999)
	for _, process := range app.Processes {
		percentCpu = percentCpu + float64(process.PercentCpu/100)
		percentMem = percentMem + float64(process.PercentMemory/100)
	}

	// TODO. It is risking here...If NPE occurs, system may break. May need to do find a better way or do nil checking.
	hostingMachineSpec, _ := app.HostingContainer.HostingPod.HostingMachine.GetMachineSpec()

	// Get the node Cpu and Mem capacity.
	nodeCpuCapacity := hostingMachineSpec.CpuCapacity
	nodeMemCapacity := hostingMachineSpec.MemCapacity

	return &ApplicationStats{
		TransactionUsed: transactionUsed,
		CpuUsed:         percentCpu * nodeCpuCapacity,
		MemUsed:         percentMem * nodeMemCapacity,
	}, nil

}
