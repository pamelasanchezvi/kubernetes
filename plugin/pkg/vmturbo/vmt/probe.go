package vmt

import (
	"fmt"
	"strconv"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"

	vmtAdvisor "k8s.io/kubernetes/plugin/pkg/vmturbo/vmt/vmtadvisor"

	"github.com/golang/glog"
	// "github.com/google/cadvisor/info/v1"
	// info "github.com/google/cadvisor/info/v2"
	"github.com/vmturbo/vmturbo-go-sdk/sdk"
)

var containerSet map[string]*vmtAdvisor.Container = make(map[string]*vmtAdvisor.Container)

// hostSet stores current discovered node infomation
var hostSet map[string]*vmtAdvisor.Host

// podSet stores current discoverd pod information
var podSet map[string]*vmtAdvisor.PodInfo

type KubeProbe struct {
	kubeClient *client.Client
}

// Parse all the node inside Kubernetes cluster. Return a list of EntityDTOs.
func (kubeProbe *KubeProbe) ParseNode() (result []*sdk.EntityDTO, err error) {
	k8sNodes, err := kubeProbe.getAllNodes()
	if err != nil {
		return
	}
	result, err = kubeProbe.parseNodeFromK8s(k8sNodes)
	return
}

// Get all nodes inside current Kubernetes cluster
func (kubeProbe *KubeProbe) getAllNodes() ([]*api.Node, error) {
	nodeList, err := kubeProbe.kubeClient.Nodes().List(labels.Everything(), fields.Everything())
	if err != nil {
		return nil, fmt.Errorf("Error getting all the nodes inside current Kubernetes cluster: %s", err)
	}
	var nodeItems []*api.Node
	for _, node := range nodeList.Items {
		n := node
		nodeItems = append(nodeItems, &n)
	}
	glog.V(3).Infof("Discovering Nodes... The cluster has " + strconv.Itoa(len(nodeItems)) + " nodes")
	return nodeItems, nil
}

// Parse nodes inside K8s. Get the resources usage of each node and build entityDTO list.
func (kubeProbe *KubeProbe) parseNodeFromK8s(nodes []*api.Node) (result []*sdk.EntityDTO, err error) {
	glog.V(3).Infof("Now parse nodes and build entityDTOs")

	// It should clear the map when a new node discovering starts.
	hostSet = make(map[string]*vmtAdvisor.Host)

	for _, node := range nodes {
		// Build the cAdvisor host
		host := vmtAdvisor.NewCadvisorHost(node)

		// Get machine specification of the current node.
		machineSpec, err := host.GetMachineSpec()
		if err != nil {
			glog.Errorf("Error: %s", err)
			continue
		}

		// TODO. Only add parsed host? or even put after getMachineCUrrentSpec?
		hostSet[node.Name] = host

		// Get machine current resource usage status.
		machineStats, err := host.GetMachineCurrentStats()
		if err != nil {
			glog.Errorf("Error: %s", err)
			continue
		}

		glog.V(3).Infof("Discovered node is %s.", node.Name)
		glog.V(4).Infof("Node CPU capacity is %f, Mem capacity is %f.", machineSpec.CpuCapacity, machineSpec.MemCapacity)

		// build node entitydto
		newNodeEntityDTO := buildNodeEntityDTO(node, machineSpec, machineStats)

		result = append(result, newNodeEntityDTO)

		// For locally test, create a fake node entityDTO
		fakeNodeEntityDTO := createFakeNodeEntityDTO(machineSpec)
		result = append(result, fakeNodeEntityDTO)
	}

	// for _, entityDto := range result {
	// 	glog.V(4).Infof("Node EntityDTO: " + entityDto.GetDisplayName())
	// 	for _, c := range entityDto.CommoditiesSold {
	// 		glog.V(5).Infof("Node commodity type is " + strconv.Itoa(int(c.GetCommodityType())) + "\n")
	// 	}
	// }

	return
}

// Build EntityDTO based on the given node data, specification and current status.
func buildNodeEntityDTO(node *api.Node, machineSpec *vmtAdvisor.MachineSpec,
	machineStats *vmtAdvisor.MachineStats) *sdk.EntityDTO {
	// Now start to build supply chain.
	nodeEntityType := sdk.EntityDTO_VIRTUAL_MACHINE
	id := node.Name
	dispName := node.Name
	entityDTOBuilder := sdk.NewEntityDTOBuilder(nodeEntityType, id)

	//machineUid := node.Status.NodeInfo.MachineID

	// Build the entityDTO.
	entityDTOBuilder = entityDTOBuilder.DisplayName(dispName)
	entityDTOBuilder = entityDTOBuilder.Sells(sdk.CommodityDTO_MEM_ALLOCATION, "Container").
		Capacity(float64(machineSpec.MemCapacity)).Used(machineStats.MemUsed)
	entityDTOBuilder = entityDTOBuilder.Sells(sdk.CommodityDTO_CPU_ALLOCATION, "Container").
		Capacity(float64(machineSpec.CpuCapacity)).Used(machineStats.CpuUsed)
	entityDTOBuilder = entityDTOBuilder.Sells(sdk.CommodityDTO_VMEM, id).
		Capacity(float64(machineSpec.MemCapacity)).Used(machineStats.MemUsed)
	entityDTOBuilder = entityDTOBuilder.Sells(sdk.CommodityDTO_VCPU, id).
		Capacity(float64(machineSpec.CpuCapacity)).Used(machineStats.CpuUsed)

	// Current we do not get physical machine info.
	//entityDTOBuilder = entityDTOBuilder.setProvider(EntityDTOS_PhysicalMachine, machineUid)
	//entityDTOBuilder = entityDTOBuilder.buys(CommodityDTOS_CPU, "", cpuUsed)
	//entityDTOBuilder = entityDTOBuilder.buys(CommodityDTOS_Mem, "", memUsed)

	entityDto := entityDTOBuilder.Create()

	return entityDto
}

// This is for the purpose of testing move action locally with VMTurbo Ops Manager.
// The fake node has the same Cpu/Mem capacity but zero utilization.
func createFakeNodeEntityDTO(machineSpec *vmtAdvisor.MachineSpec) *sdk.EntityDTO {
	fakeNode := &api.Node{}
	fakeNode.Name = "1.1.1.1"

	fakeMachineStats := &vmtAdvisor.MachineStats{
		CpuUsed: float64(0),
		MemUsed: float64(0),
	}
	return buildNodeEntityDTO(fakeNode, machineSpec, fakeMachineStats)
}

// Parse pods those are defined in namespace.
func (kubeProbe *KubeProbe) ParsePod(namespace string) (result []*sdk.EntityDTO, err error) {
	k8sPods, err := kubeProbe.getAllPods(namespace)
	if err != nil {
		return nil, err
	}
	result, err = kubeProbe.parsePodFromK8s(k8sPods)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Retrieve all the pods defined in namespace
func (kubeProbe *KubeProbe) getAllPods(namespace string) ([]*api.Pod, error) {
	var podItems []*api.Pod

	podList, err := kubeProbe.kubeClient.Pods(namespace).List(labels.Everything(), fields.Everything())
	if err != nil {
		return nil, fmt.Errorf("Error getting all the pods in %s: %s", namespace, err)
	}
	for _, pod := range podList.Items {
		p := pod
		podItems = append(podItems, &p)
	}
	glog.V(3).Infof("Discovering Pods.. Now the cluster has " + strconv.Itoa(len(podItems)) + " pods")

	return podItems, nil
}

// Parse each pods inside K8s. Get the resources usage of each pod and build entityDTO.
func (kubeProbe *KubeProbe) parsePodFromK8s(pods []*api.Pod) (result []*sdk.EntityDTO, err error) {
	glog.V(3).Infof("Now getting pod info from cAdvisor")

	// In order to reuse hostSet, we must make sure node is parsed first.
	if hostSet == nil || len(hostSet) < 1 {
		glog.Warningf("No node in Kubernetes has been probe. Make sure parse node first. Now parse nodes...")
		// TODO. Should we start get all the host here?
		return nil, fmt.Errorf("No cAdvisor host found. Node must be parsed first.")
	}

	// Pre-parse pod according to host
	podInfoSet, err := preParsePod()
	if err != nil {
		return nil, err
	}
	podSet = podInfoSet

	// Third, get accumulative usage data for each pod according to the underlying containers
	glog.V(3).Infof("Now parse Pods")
	for _, pod := range pods {
		podNameWithNamespace := pod.Namespace + "/" + pod.Name

		// Must make sure the pod is observerable by cAdvisor
		podInfo, exist := podInfoSet[podNameWithNamespace]
		if !exist {
			// If does not exist, ignore the pod.
			continue
		}

		podSpec, err := podInfo.GetPodSpec(pod)
		if err != nil {
			return nil, err
		}

		glog.V(3).Infof("PodSpec of %s is: %s ", podNameWithNamespace, podSpec)

		podStats, err := podInfo.GetPodCurrentStats()
		if err != nil {
			return nil, err
		}

		glog.V(3).Infof("PodStats of %s is: %s ", podNameWithNamespace, podStats)

		entityDto := buildPodEntityDTO(podInfo, podSpec, podStats)
		glog.V(3).Infof("Pod EntityDTO: " + entityDto.GetDisplayName())

		result = append(result, entityDto)
	}

	for _, entityDto := range result {
		glog.V(3).Infof("Pod EntityDTO: " + entityDto.GetDisplayName())
	}

	return
}

// In this method, we iterate all the host, getting containers running on each host through cAdvisor.
// The container info returned by cAdvisor contains which pod the container is running in and real time
// resource usage. Then containers are grouped based on pods.
// Return a map of PodInfo. Key is pod name(with namespace), value is the actual PodInfo instance.
func preParsePod() (map[string]*vmtAdvisor.PodInfo, error) {
	// TODO, need to make sure host set is not nil or empty.
	if hostSet == nil || len(hostSet) < 1 {
		//TODO. Need to go back to discover nodes.
	}

	var podInfoSet map[string]*vmtAdvisor.PodInfo = make(map[string]*vmtAdvisor.PodInfo)

	// For each host there should be a cAdvisor client.
	for _, host := range hostSet {

		// use cadvisor to get all containers on that host
		cadvisor := &vmtAdvisor.CadvisorSource{}

		// Here we do not need root container.
		subcontainers, _, err := cadvisor.GetAllContainers(host, time.Now(), time.Now())
		if err != nil {
			glog.Errorf("Error during preparse Pod in %s: %s", host.Name, err)
		}

		// Map container to each pod. Key is pod name, value is container.
		for _, container := range subcontainers {
			spec := container.Spec
			if &spec != nil && spec.Labels != nil {
				// TODO! hardcoded here. Works but not good. Maybe can find better solution?
				// There is a risk that if cAdvisor changes the info containing in this field, then it will fail.
				// The value returned here is namespace/name of pod
				if podName, ok := spec.Labels["io.kubernetes.pod.name"]; ok {
					glog.V(3).Infof("Container %s is in Pod %s", container.Name, podName)
					podInfo, exist := podInfoSet[podName]
					if !exist {
						podInfo = vmtAdvisor.NewPodInfo(podName, host)
					}

					var containers []*vmtAdvisor.Container
					if podInfo.Containers != nil {
						containers = podInfo.Containers
					}

					containers = append(containers, container)

					podInfo.Containers = containers
					container.HostingPod = podInfo

					podInfoSet[podName] = podInfo

					// store in containerSet
					containerSet[container.Name] = container
				}
			}
		}
	}
	return podInfoSet, nil
}

// Build EntityDTO based on the given node data, specification and current status.
func buildPodEntityDTO(pod *vmtAdvisor.PodInfo, podSpec *vmtAdvisor.PodSpec,
	podStats *vmtAdvisor.PodStats) *sdk.EntityDTO {
	// Use pod as Application for now
	podEntityType := sdk.EntityDTO_CONTAINER_POD

	id := pod.ID
	dispName := pod.ID
	entityDTOBuilder := sdk.NewEntityDTOBuilder(podEntityType, id)

	minionId := pod.HostingMachine.Name
	if minionId == "" {
		// At this point the pod should have a hosting minion. If not, then there is something wrong.
		// TODO! A warning or error?
		glog.Errorf("NodeID is not found for %s. Something wrong.", pod.ID)
		return nil
	}
	glog.V(3).Infof("Hosting Minion ID for %s is %s", dispName, minionId)

	glog.V(4).Infof("The actual Cpu used value of %s is %f", id, podStats.CpuUsed)
	glog.V(4).Infof("The actual Mem used value of %s is %f", id, podStats.MemUsed)

	entityDTOBuilder = entityDTOBuilder.DisplayName(dispName)
	entityDTOBuilder = entityDTOBuilder.Sells(sdk.CommodityDTO_MEM_ALLOCATION, pod.ID).Capacity(podSpec.MemCapacity).Used(podSpec.MemCapacity * 0.8)
	entityDTOBuilder = entityDTOBuilder.Sells(sdk.CommodityDTO_CPU_ALLOCATION, pod.ID).Capacity(podSpec.CpuCapacity).Used(podSpec.CpuCapacity * 0.8)
	entityDTOBuilder = entityDTOBuilder.SetProvider(sdk.EntityDTO_VIRTUAL_MACHINE, minionId)
	entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_CPU_ALLOCATION, "Container", podSpec.CpuCapacity)
	entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_MEM_ALLOCATION, "Container", podSpec.MemCapacity)

	entityDto := entityDTOBuilder.Create()

	return entityDto
}

var pod2AppMap map[string]map[string]*vmtAdvisor.Application = make(map[string]map[string]*vmtAdvisor.Application)

// Parse processes those are defined in namespace.
func (kubeProbe *KubeProbe) ParseApplication(namespace string) (result []*sdk.EntityDTO, err error) {
	glog.Infof("Has %d hosts", len(hostSet))

	for nodeName, host := range hostSet {
		glog.Infof("Now getting process in host %s", nodeName)

		appMap, err := preParseApplication(host)
		if err != nil {
			glog.Errorf("Error getting application on host %s during preparsing: %s", nodeName, err)
		}

		for _, app := range appMap {
			// glog.Infof("pod %s has the following application %s", podName, app.Cmd)

			appSpec, err := app.GetApplicationSpec()
			if err != nil {
				glog.Errorf("Error getting application specification:%s", err)
				continue
			}

			appStats, err := app.GetApplicationCurrentStats()
			if err != nil {
				glog.Errorf("Error getting application stats:%s", err)
				continue
			}

			entityDto := buildApplicationEntityDTO(app, appSpec, appStats)

			result = append(result, entityDto)

		}
	}
	return
}

func preParseApplication(host *vmtAdvisor.Host) (map[string]*vmtAdvisor.Application, error) {
	// use cadvisor to get all process on that host
	cadvisor := &vmtAdvisor.CadvisorSource{}
	psInfors, err := cadvisor.GetProcessInfo(*host)
	if err != nil {
		glog.Errorf("Error parsing process %s", err)
		return nil, err
	}

	apps := make(map[string]*vmtAdvisor.Application)

	if len(psInfors) < 1 {
		glog.Warningf("No process find")
		return apps, nil
	}

	// app.Cmd + "::" + podName

	// Applicaiton->Name is processName::podName
	// Application->ProcessName is process.cmd

	for _, process := range psInfors {
		// Here cgroupPath for a process is the same with the container name.
		// Again, if cAdvisor uses a different thing, the app parser would fail.
		cgroupPath := process.CgroupPath
		if container, exist := containerSet[cgroupPath]; exist {
			podName := container.HostingPod.ID
			glog.V(4).Infof("%s is in pod %s", process.Cmd, podName)

			processName := process.Cmd

			appName := processName + "::" + podName

			app, hasApp := apps[appName]
			if !hasApp {
				app = vmtAdvisor.NewApplication(appName, processName, container)
			}
			processList := app.Processes
			processList = append(processList, process)

			app.Processes = processList
			apps[appName] = app

			// append app to container's ApplicationList
			container.AddApplication(app)
		}
	}
	return apps, nil
}

func buildApplicationEntityDTO(app *vmtAdvisor.Application, appSpec *vmtAdvisor.ApplicationSpec, appStats *vmtAdvisor.ApplicationStats) *sdk.EntityDTO {
	appEntityType := sdk.EntityDTO_APPLICATION
	id := app.Name
	//app.Cmd + "::" + podName
	dispName := app.Name
	entityDTOBuilder := sdk.NewEntityDTOBuilder(appEntityType, id)

	//TODO. NPE risk. May need nil check.
	hostingPod := app.HostingContainer.HostingPod
	hostingPodName := hostingPod.ID

	hostingMachineName := hostingPod.HostingMachine.Name

	entityDTOBuilder = entityDTOBuilder.DisplayName(dispName)
	entityDTOBuilder = entityDTOBuilder.Sells(sdk.CommodityDTO_TRANSACTION, app.ProcessName).
		Capacity(appSpec.TransactionCapacity).Used(appStats.TransactionUsed)
	entityDTOBuilder = entityDTOBuilder.SetProvider(sdk.EntityDTO_CONTAINER_POD, hostingPodName)
	entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_CPU_ALLOCATION, hostingPodName, appStats.CpuUsed)
	entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_MEM_ALLOCATION, hostingPodName, appStats.MemUsed)
	entityDTOBuilder = entityDTOBuilder.SetProvider(sdk.EntityDTO_VIRTUAL_MACHINE, hostingMachineName)
	entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_VCPU, hostingMachineName, appStats.CpuUsed)
	entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_VMEM, hostingMachineName, appStats.MemUsed)

	entityDto := entityDTOBuilder.Create()

	appType := app.ProcessName
	// ipAddress := ""
	appData := &sdk.EntityDTO_ApplicationData{
		Type: &appType,
		// IpAddress: &ipAddress,
	}
	entityDto.ApplicationData = appData
	return entityDto
}

// Parse Services inside Kubernetes and build entityDTO as VApp.
func (kubeProbe *KubeProbe) ParseService(namespace string, selector labels.Selector) (result []*sdk.EntityDTO, err error) {
	serviceList, err := kubeProbe.kubeClient.Services(namespace).List(selector)
	if err != nil {
		return nil, fmt.Errorf("Error listing services: %s", err)
	}

	endpointList, err := kubeProbe.kubeClient.Endpoints(namespace).List(selector)
	if err != nil {
		return nil, fmt.Errorf("Error listing endpoints: %s", err)
	}
	// first make a endpoint map, key is endpoints label, value is endoint object
	endpointMap := make(map[string]api.Endpoints)
	for _, endpoint := range endpointList.Items {
		nameWithNamespace := endpoint.Namespace + "/" + endpoint.Name
		endpointMap[nameWithNamespace] = endpoint
	}

	// key is service identifier, value is the string list of the pod name with namespace
	serviceEndpointMap := make(map[string][]string)
	for _, service := range serviceList.Items {
		serviceNameWithNamespace := service.Namespace + "/" + service.Name
		serviceEndpoint := endpointMap[serviceNameWithNamespace]
		subsets := serviceEndpoint.Subsets
		for _, endpointSubset := range subsets {
			addresses := endpointSubset.Addresses
			for _, address := range addresses {
				target := address.TargetRef
				if target == nil {
					continue
				}
				podName := target.Name
				podNamespace := target.Namespace
				podNameWithNamespace := podNamespace + "/" + podName
				// get the pod name and the service name
				var podIDList []string
				if pList, exists := serviceEndpointMap[serviceNameWithNamespace]; exists {
					podIDList = pList
				}
				podIDList = append(podIDList, podNameWithNamespace)
				serviceEndpointMap[serviceNameWithNamespace] = podIDList
			}
		}

		glog.V(5).Infof("podSet has the following pod %s", podSet)

		// Now build entityDTO
		for serviceID, podIDList := range serviceEndpointMap {
			glog.Infof("service %s has the following pod as endpoints %s", serviceID, podIDList)

			if len(podIDList) < 1 {
				continue
			}
			// Must make sure what processes are in a service.
			servicePodInfo := podSet[podIDList[0]]

			var applicationForCurrentService []*vmtAdvisor.Application
			for _, container := range servicePodInfo.Containers {
				glog.V(5).Infof("%s has the following container %s", servicePodInfo.ID, container.Name)

				for _, app := range container.Applications {
					glog.V(5).Infof("Container has applicaitons are %s", app.Name)

					applicationForCurrentService = append(applicationForCurrentService, app)

				}
			}

			for _, application := range applicationForCurrentService {
				// first find out what processes are in this service
				serviceEntityType := sdk.EntityDTO_VIRTUAL_APPLICATION
				appName := application.ProcessName
				id := "vApp-" + appName
				dispName := id
				entityDTOBuilder := sdk.NewEntityDTOBuilder(serviceEntityType, id)

				entityDTOBuilder = entityDTOBuilder.DisplayName(dispName)
				for _, podID := range podIDList {
					entityDTOBuilder = entityDTOBuilder.SetProvider(sdk.EntityDTO_CONTAINER_POD, appName+"::"+podID)
					entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_TRANSACTION, appName, 9999)

				}

				entityDto := entityDTOBuilder.Create()

				glog.V(3).Infof("created a service entityDTO", entityDto)
				result = append(result, entityDto)
			}
		}
	}

	return
}

// Show container stats for each container. For debug and troubleshooting purpose.
func showContainerStats(container *vmtAdvisor.Container) {
	glog.V(3).Infof("Host name %s", container.Hostname)
	glog.V(3).Infof("Container name is %s", container.Name)
	containerStats := container.Stats
	currentStat := containerStats[len(containerStats)-1]
	glog.V(3).Infof("CPU usage is %d", currentStat.Cpu.Usage.Total)
	glog.V(3).Infof("MEM usage is %d", currentStat.Memory.Usage)
}
