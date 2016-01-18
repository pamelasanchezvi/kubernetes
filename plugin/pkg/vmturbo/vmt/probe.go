package vmt

import (
	"fmt"
	"strconv"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"

	vmtAdvisor "k8s.io/kubernetes/plugin/pkg/vmturbo/vmt/cadvisor"

	"github.com/golang/glog"
	"github.com/google/cadvisor/info/v1"
	info "github.com/google/cadvisor/info/v2"
	"github.com/vmturbo/vmturbo-go-sdk/sdk"
)

var container2PodMap map[string]string = make(map[string]string)
var hostSet map[string]*vmtAdvisor.Host = make(map[string]*vmtAdvisor.Host)

var nodeUidTranslationMap map[string]string = make(map[string]string)

type KubeProbe struct {
	kubeClient *client.Client
}

func (kubeProbe *KubeProbe) ParseNode() (result []*sdk.EntityDTO, err error) {
	k8sNodes := kubeProbe.getAllNodes()
	result, err = kubeProbe.parseNodeFromK8s(k8sNodes)
	return
}

// Get all nodes
func (kubeProbe *KubeProbe) getAllNodes() []*api.Node {
	nodeList, err := kubeProbe.kubeClient.Nodes().List(labels.Everything(), fields.Everything())
	if err != nil {
		return nil
	}
	var nodeItems []*api.Node
	for _, node := range nodeList.Items {
		n := node
		nodeItems = append(nodeItems, &n)
	}
	glog.V(3).Infof("Discovering Nodes.. The cluster has " + strconv.Itoa(len(nodeItems)) + " nodes")
	return nodeItems
}

// Parse each node inside K8s. Get the resources usage of each node and build the entityDTO.
func (kubeProbe *KubeProbe) parseNodeFromK8s(nodes []*api.Node) (result []*sdk.EntityDTO, err error) {
	glog.V(3).Infof("---------- Now parse Node ----------")

	for _, node := range nodes {
		// First, use cAdvisor to get node info
		//machineId := node.Status.NodeInfo.MachineID
		var nodeIP string
		nodeAddresses := node.Status.Addresses
		for _, nodeAddress := range nodeAddresses {
			// TODO Tests is needed if this is the correct address to be used.
			if nodeAddress.Type == api.NodeLegacyHostIP {
				nodeIP = nodeAddress.Address
			}
		}

		// use cadvisor to get machine info
		cadvisor := &vmtAdvisor.CadvisorSource{}
		host := &vmtAdvisor.Host{
			IP:       nodeIP,
			Port:     4194,
			Resource: "",
		}

		hostSet[node.Name] = host

		machineInfo, err := cadvisor.GetMachineInfo(*host)
		if err != nil {
			continue
			// return nil, err
		}
		// The return cpu frequency is in KHz, we need MHz
		cpuFrequency := machineInfo.CpuFrequency / 1000

		// Here we only need the root container.
		_, root, err := cadvisor.GetAllContainers(*host, time.Now(), time.Now())
		if err != nil {
			return nil, err
		}
		containerStats := root.Stats
		// To get a valid cpu usage, there must be at least 2 valid stats.
		if len(containerStats) < 2 {
			glog.Warning("Not enough data")
			continue
			// return nil, fmt.Errorf("Not enough status data of current node %s.", nodeIP)
		}
		currentStat := containerStats[len(containerStats)-1]
		prevStat := containerStats[len(containerStats)-2]
		rawUsage := int64(currentStat.Cpu.Usage.Total - prevStat.Cpu.Usage.Total)
		glog.V(4).Infof("rawUsage is %d", rawUsage)
		intervalInNs := currentStat.Timestamp.Sub(prevStat.Timestamp).Nanoseconds()
		glog.V(4).Infof("interval is %d", intervalInNs)
		rootCurCpu := float64(rawUsage) * 1.0 / float64(intervalInNs)
		rootCurMem := float64(currentStat.Memory.Usage) / 1024 // Mem is returned in B

		// Get the node Cpu and Mem capacity.
		nodeCpuCapacity := float64(machineInfo.NumCores) * float64(cpuFrequency)
		nodeMemCapacity := float64(machineInfo.MemoryCapacity) / 1024 // Mem is returned in B
		glog.V(3).Infof("Discovered node is " + node.Name)
		glog.V(4).Infof("Node CPU capacity is %f", nodeCpuCapacity)
		glog.V(4).Infof("Node Mem capacity is %f", nodeMemCapacity)

		// Now start to build supply chain.
		nodeEntityType := sdk.EntityDTO_VIRTUAL_MACHINE
		id := string(node.UID)
		dispName := node.Name
		nodeUidTranslationMap[nodeIP] = id
		entityDTOBuilder := sdk.NewEntityDTOBuilder(nodeEntityType, id)

		// Find out the used value for each commodity
		cpuUsed := float64(rootCurCpu) * float64(cpuFrequency)
		memUsed := float64(rootCurMem)

		cpuUsed = float64(8000)

		// machineUid := node.Status.NodeInfo.MachineID

		// Build the entityDTO.
		entityDTOBuilder = entityDTOBuilder.DisplayName(dispName)
		entityDTOBuilder = entityDTOBuilder.Sells(sdk.CommodityDTO_MEM_ALLOCATION, "Container").
			Capacity(float64(nodeMemCapacity)).Used(memUsed)
		entityDTOBuilder = entityDTOBuilder.Sells(sdk.CommodityDTO_CPU_ALLOCATION, "Container").
			Capacity(float64(nodeCpuCapacity)).Used(cpuUsed)
		entityDTOBuilder = entityDTOBuilder.Sells(sdk.CommodityDTO_VMEM, id).
			Capacity(float64(nodeMemCapacity)).Used(memUsed)
		entityDTOBuilder = entityDTOBuilder.Sells(sdk.CommodityDTO_VCPU, id).
			Capacity(float64(nodeCpuCapacity)).Used(cpuUsed)

		// entityDTOBuilder = entityDTOBuilder.SetProperty("ipAddress", "10.10.173.131")
		nodeIP2 := "10.10.173.131"
		entityDTOBuilder = entityDTOBuilder.SetProperty("ipAddress", nodeIP2)
		glog.V(3).Infof("Parse pod: The ip of vm to be stitched is %s", nodeIP)

		// entityDTOBuilder = entityDTOBuilder.SetProvider(sdk.EntityDTO_PHYSICAL_MACHINE, machineUid)
		// entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_CPU, "", cpuUsed)
		// entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_MEM, "", memUsed)

		replacementEntityMetaDataBuilder := sdk.NewReplacementEntityMetaDataBuilder()
		replacementEntityMetaDataBuilder.Matching(sdk.SUPPLYCHAIN_CONSTANT_IP_ADDRESS)
		replacementEntityMetaDataBuilder.PatchSelling(sdk.CommodityDTO_CPU_ALLOCATION)
		replacementEntityMetaDataBuilder.PatchSelling(sdk.CommodityDTO_MEM_ALLOCATION)

		replacementEntityMetaDataBuilder.PatchSelling(sdk.CommodityDTO_VCPU)
		replacementEntityMetaDataBuilder.PatchSelling(sdk.CommodityDTO_VMEM)
		// replacementEntityMetaDataBuilder.PatchBuying(sdk.CommodityDTO_VCPU)
		// replacementEntityMetaDataBuilder.PatchBuying(sdk.CommodityDTO_VMEM)
		metaData := replacementEntityMetaDataBuilder.Build()

		entityDTOBuilder = entityDTOBuilder.ReplacedBy(metaData)

		entityDto := entityDTOBuilder.Create()
		result = append(result, entityDto)

		// // create a fake VM
		// entityDTOBuilder2 := sdk.NewEntityDTOBuilder(nodeEntityType, "1.1.1.1")
		// // Find out the used value for each commodity
		// cpuUsed = float64(0)
		// memUsed = float64(0)
		// // Build the entityDTO.
		// entityDTOBuilder2 = entityDTOBuilder2.DisplayName("1.1.1.1")
		// entityDTOBuilder2 = entityDTOBuilder2.Sells(sdk.CommodityDTO_MEM_ALLOCATION, "Container").
		// 	Capacity(float64(nodeMemCapacity)).Used(memUsed)
		// entityDTOBuilder2 = entityDTOBuilder2.Sells(sdk.CommodityDTO_CPU_ALLOCATION, "Container").
		// 	Capacity(float64(nodeCpuCapacity)).Used(cpuUsed)
		// entityDTOBuilder2 = entityDTOBuilder2.Sells(sdk.CommodityDTO_VMEM, "1.1.1.1").
		// 	Capacity(float64(nodeMemCapacity)).Used(memUsed)
		// entityDTOBuilder2 = entityDTOBuilder2.Sells(sdk.CommodityDTO_VCPU, "1.1.1.1").
		// 	Capacity(float64(nodeCpuCapacity)).Used(cpuUsed)
		// entityDTOBuilder2 = entityDTOBuilder2.SetProperty("ipAddress", "10.10.173.196")

		// replacementEntityMetaDataBuilder2 := sdk.NewReplacementEntityMetaDataBuilder()
		// replacementEntityMetaDataBuilder2.Matching(sdk.SUPPLYCHAIN_CONSTANT_IP_ADDRESS)
		// replacementEntityMetaDataBuilder2.PatchSelling(sdk.CommodityDTO_CPU_ALLOCATION)
		// replacementEntityMetaDataBuilder2.PatchSelling(sdk.CommodityDTO_MEM_ALLOCATION)
		// replacementEntityMetaDataBuilder2.PatchSelling(sdk.CommodityDTO_VCPU)
		// replacementEntityMetaDataBuilder2.PatchSelling(sdk.CommodityDTO_VMEM)
		// metaData2 := replacementEntityMetaDataBuilder2.Build()

		// entityDTOBuilder2 = entityDTOBuilder2.ReplacedBy(metaData2)
		// entityDto2 := entityDTOBuilder2.Create()
		// result = append(result, entityDto2)
	}

	for _, entityDto := range result {
		glog.V(4).Infof("Node EntityDTO: " + entityDto.GetDisplayName())
		for _, c := range entityDto.CommoditiesSold {
			glog.V(5).Infof("Node commodity type is " + strconv.Itoa(int(c.GetCommodityType())) + "\n")
		}
	}

	return
}

// Parse pods those are defined in namespace.
func (kubeProbe *KubeProbe) ParsePod(namespace string) (result []*sdk.EntityDTO, err error) {
	k8sPods := kubeProbe.getAllPods(namespace)
	result, err = kubeProbe.parsePodFromK8s(k8sPods)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Retrieve all the pods defined in namespace
func (kubeProbe *KubeProbe) getAllPods(namespace string) []*api.Pod {
	var podItems []*api.Pod

	podList, err := kubeProbe.kubeClient.Pods(namespace).List(labels.Everything(), fields.Everything())
	if err != nil {
		return nil
	}
	for _, pod := range podList.Items {
		p := pod
		podItems = append(podItems, &p)
	}
	glog.V(3).Infof("Discovering Pods, now the cluster has " + strconv.Itoa(len(podItems)) + " pods")

	return podItems
}

func (kubeProbe *KubeProbe) parsePodFromK8s(pods []*api.Pod) (result []*sdk.EntityDTO, err error) {
	glog.V(3).Infof("Now try with cAdvisor")

	// First, group pods according to the ip of their hosting nodes
	nodePodsMap := make(map[string][]*api.Pod)
	for _, pod := range pods {
		hostIP := pod.Status.HostIP
		var podList []*api.Pod
		if list, ok := nodePodsMap[hostIP]; ok {
			podList = list
		}
		podList = append(podList, pod)
		nodePodsMap[hostIP] = podList
	}

	//Second, process pods according to each node
	podContainers := make(map[string][]*vmtAdvisor.Container)

	// a pod to Node map
	podNodeMap := make(map[string]*v1.MachineInfo)

	// For each host there should be a cAdvisor client.
	for hostIP, _ := range nodePodsMap {

		// use cadvisor to get all containers on that host
		cadvisor := &vmtAdvisor.CadvisorSource{}

		// TODO, must check the port number for cAdvisor client is static.
		host := &vmtAdvisor.Host{
			IP:       hostIP,
			Port:     4194,
			Resource: "",
		}

		// Here we do not need root container.
		subcontainers, _, err := cadvisor.GetAllContainers(*host, time.Now(), time.Now())
		if err != nil {
			return nil, err
		}

		machineInfo, err := cadvisor.GetMachineInfo(*host)
		if err != nil {
			continue
			// return nil, err
		}

		// Map container to each pod. Key is pod name, value is container.
		for _, container := range subcontainers {
			spec := container.Spec
			if &spec != nil && container.Spec.Labels != nil {
				// TODO! hardcoded here. Works but not good. Maybe can find better solution?
				// The value returned here is namespace/podname
				if podName, ok := container.Spec.Labels["io.kubernetes.pod.name"]; ok {
					glog.V(4).Infof("Container %s is in Pod %s", container.Name, podName)
					var containers []*vmtAdvisor.Container
					if ctns, exist := podContainers[podName]; exist {
						containers = ctns
					}
					containers = append(containers, container)
					podContainers[podName] = containers

					// store in container2PodMap
					container2PodMap[container.Name] = podName

					// Store in pod node map.
					if _, exist := podNodeMap[podName]; !exist {
						podNodeMap[podName] = machineInfo
					}
				}
			}
		}
	}

	// Third, get accumulative usage data for each pod according to the underlying containers
	glog.V(3).Infof("Now parse Pods")
	for _, pod := range pods {
		cpuCapacity := int64(0)
		memCapacity := int64(0)

		// TODO! Here we assume when user defines a pod, resource requirements are also specified.
		// The metrics we care about now are Cpu and Mem.
		for _, container := range pod.Spec.Containers {
			requests := container.Resources.Limits
			memCapacity += requests.Memory().Value()
			cpuCapacity += requests.Cpu().MilliValue()
		}

		podNameWithNamespace := pod.Namespace + "/" + pod.Name

		if _, exist := podNodeMap[podNameWithNamespace]; !exist {
			glog.Warningf("Cannot find pod %s in podNodeMap", pod.Name)
			continue
		}

		// get cpu frequency and convert KHz to MHz
		cpuFrequency := podNodeMap[podNameWithNamespace].CpuFrequency / 1000

		// the cpu return value is in core*1000, so here should divide 1000
		podCpuCapacity := float64(cpuCapacity) / 1000 * float64(cpuFrequency)
		podMemCapacity := float64(memCapacity) / 1024 // Mem is in bytes, convert to Kb
		glog.V(4).Infof("Cpu cap of Pod %s is %f", pod.Name, podCpuCapacity)
		glog.V(4).Infof("Mem cap of Pod %s is %f", pod.Name, podMemCapacity)

		podCpuUsed := float64(0)
		podMemUsed := float64(0)

		if containers, ok := podContainers[podNameWithNamespace]; ok {
			for _, container := range containers {
				containerStats := container.Stats
				if len(containerStats) < 2 {
					//TODO, maybe a warning is enough?
					glog.Warningf("Not enough data for %s", podNameWithNamespace)
					continue
					// return nil, fmt.Errorf("Not enough data for %s", podNameWithNamespace)
				}
				currentStat := containerStats[len(containerStats)-1]
				prevStat := containerStats[len(containerStats)-2]
				rawUsage := int64(currentStat.Cpu.Usage.Total - prevStat.Cpu.Usage.Total)
				intervalInNs := currentStat.Timestamp.Sub(prevStat.Timestamp).Nanoseconds()
				podCpuUsed += float64(rawUsage) * 1.0 / float64(intervalInNs)
				podMemUsed += float64(currentStat.Memory.Usage)
			}
		} else {
			glog.Warningf("Cannot find pod %s", pod.Name)
			continue
		}

		// convert num of core to frequecy in MHz
		podCpuUsed = podCpuUsed * float64(cpuFrequency)
		podMemUsed = podMemUsed / 1024 // Mem is in bytes, convert to Kb

		glog.V(3).Infof(" Discovered pod is " + pod.Name)
		// TODO, not sure if this conversion is correct.
		glog.V(4).Infof(" Pod %s CPU request is %f", pod.Name, podCpuUsed)
		glog.V(4).Infof(" Pod %s Mem request is %f", pod.Name, podMemUsed)

		// Use pod as Application for now
		podEntityType := sdk.EntityDTO_CONTAINER_POD
		id := podNameWithNamespace
		dispName := podNameWithNamespace
		entityDTOBuilder := sdk.NewEntityDTOBuilder(podEntityType, id)

		minionId := pod.Spec.NodeName
		if minionId == "" {
			// At this point the pod should have a hosting minion. If not, then there is something wrong.
			// TODO! A warning or error?
		}
		glog.V(3).Infof("Hosting Minion ID for %s is %s", dispName, minionId)

		glog.V(4).Infof("The actual Cpu used value of %s is %f", id, podCpuUsed)
		glog.V(4).Infof("The actual Mem used value of %s is %f", id, podMemUsed)

		entityDTOBuilder = entityDTOBuilder.DisplayName(dispName)
		entityDTOBuilder = entityDTOBuilder.Sells(sdk.CommodityDTO_MEM_ALLOCATION, podNameWithNamespace).Capacity(podMemCapacity).Used(podMemCapacity) // * 0.8)
		entityDTOBuilder = entityDTOBuilder.Sells(sdk.CommodityDTO_CPU_ALLOCATION, podNameWithNamespace).Capacity(podCpuCapacity).Used(podCpuCapacity) // * 0.8)
		providerUid := nodeUidTranslationMap[minionId]
		entityDTOBuilder = entityDTOBuilder.SetProvider(sdk.EntityDTO_VIRTUAL_MACHINE, providerUid)
		entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_CPU_ALLOCATION, "Container", podCpuCapacity)
		entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_MEM_ALLOCATION, "Container", podMemCapacity)

		// entityDTOBuilder = entityDTOBuilder.SetProperty("ipAddress", "10.10.173.131")
		mId := "10.10.173.131"
		entityDTOBuilder = entityDTOBuilder.SetProperty("ipAddress", mId)
		glog.V(3).Infof("Parse pod: The ip of vm to be stitched is %s", minionId)

		entityDto := entityDTOBuilder.Create()
		result = append(result, entityDto)
	}

	for _, entityDto := range result {
		glog.V(3).Infof("Pod EntityDTO: " + entityDto.GetDisplayName())
	}

	return
}

var pod2AppMap map[string]map[string]vmtAdvisor.Application = make(map[string]map[string]vmtAdvisor.Application)

// Parse processes those are defined in namespace.
func (kubeProbe *KubeProbe) ParseApplication(namespace string) (result []*sdk.EntityDTO, err error) {
	glog.Infof("Has %d hosts", len(hostSet))

	for nodeName, host := range hostSet {
		glog.Infof("Now get process in host %s", nodeName)
		// use cadvisor to get all process on that host
		cadvisor := &vmtAdvisor.CadvisorSource{}
		psInfors, err := cadvisor.GetProcessInfo(*host)
		if err != nil {
			glog.Errorf("Error parsing process %s", err)
			return nil, err
		}
		if len(psInfors) < 1 {
			glog.Warningf("No process find")
			return result, nil
		}

		// Now all process info have been got. Group processes to pods
		pod2ProcessesMap := make(map[string][]info.ProcessInfo)
		for _, process := range psInfors {
			// Here cgroupPath for a process is the same with the container name
			cgroupPath := process.CgroupPath
			if podName, exist := container2PodMap[cgroupPath]; exist {
				glog.V(4).Infof("%s is in pod %s", process.Cmd, podName)
				var processList []info.ProcessInfo
				if processes, hasList := pod2ProcessesMap[podName]; hasList {
					processList = processes
				}
				processList = append(processList, process)
				pod2ProcessesMap[podName] = processList
			}
		}

		for podN, processList := range pod2ProcessesMap {
			for _, process := range processList {
				glog.V(4).Infof("pod %s has the following process %s", podN, process.Cmd)
			}
		}

		// The same processes should represent the same application
		// key:podName, value: a map (key:process.Cmd, value: Application)
		pod2ApplicationMap := make(map[string]map[string]vmtAdvisor.Application)
		for podName, processList := range pod2ProcessesMap {
			if _, exists := pod2ApplicationMap[podName]; !exists {
				apps := make(map[string]vmtAdvisor.Application)
				pod2ApplicationMap[podName] = apps
			}
			applications := pod2ApplicationMap[podName]
			for _, process := range processList {
				if _, hasApp := applications[process.Cmd]; !hasApp {
					applications[process.Cmd] = vmtAdvisor.Application(process)
				} else {
					app := applications[process.Cmd]
					app.PercentCpu = app.PercentCpu + process.PercentCpu
					app.PercentMemory = app.PercentMemory + process.PercentMemory
					applications[process.Cmd] = app
				}
			}
		}

		pod2AppMap = pod2ApplicationMap

		// In order to get the actual usage for each process, the CPU/Mem capacity
		// for the machine must be retrieved.
		machineInfo, err := cadvisor.GetMachineInfo(*host)
		if err != nil {
			glog.Warningf("Error getting machine info for %s when parsing process: %s", nodeName, err)
			continue
			// return nil, err
		}
		// The return cpu frequency is in KHz, we need MHz
		cpuFrequency := machineInfo.CpuFrequency / 1000
		// Get the node Cpu and Mem capacity.
		nodeCpuCapacity := float64(machineInfo.NumCores) * float64(cpuFrequency)
		nodeMemCapacity := float64(machineInfo.MemoryCapacity) / 1024 // Mem is returned in B

		for podName, appMap := range pod2ApplicationMap {
			for _, app := range appMap {
				glog.V(4).Infof("pod %s has the following application %s", podName, app.Cmd)

				appEntityType := sdk.EntityDTO_APPLICATION
				id := app.Cmd + "::" + podName
				dispName := app.Cmd + "::" + podName
				entityDTOBuilder := sdk.NewEntityDTOBuilder(appEntityType, id)

				cpuUsage := nodeCpuCapacity * float64(app.PercentCpu/100)
				memUsage := nodeMemCapacity * float64(app.PercentMemory/100)

				glog.V(4).Infof("Percent Cpu for %s is %f, usage is %f", dispName, app.PercentCpu, cpuUsage)
				glog.V(4).Infof("Percent Mem for %s is %f, usage is %f", dispName, app.PercentMemory, memUsage)

				entityDTOBuilder = entityDTOBuilder.DisplayName(dispName)
				entityDTOBuilder = entityDTOBuilder.Sells(sdk.CommodityDTO_TRANSACTION, app.Cmd).Capacity(10000).Used(9999)
				entityDTOBuilder = entityDTOBuilder.SetProvider(sdk.EntityDTO_CONTAINER_POD, podName)
				entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_CPU_ALLOCATION, podName, cpuUsage)
				entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_MEM_ALLOCATION, podName, memUsage)

				providerUid := nodeUidTranslationMap[nodeName]
				entityDTOBuilder = entityDTOBuilder.SetProvider(sdk.EntityDTO_VIRTUAL_MACHINE, providerUid)
				entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_VCPU, providerUid, cpuUsage)
				entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_VMEM, providerUid, memUsage)
				// entityDTOBuilder = entityDTOBuilder.SetProperty("ipAddress", "10.10.173.131")

				entityDto := entityDTOBuilder.Create()

				appType := app.Cmd
				ipAddress := "10.10.173.131"
				// ipAddress := nodeName
				glog.V(3).Infof("Parse pod: The ip of vm to be stitched is %s", ipAddress)

				appData := &sdk.EntityDTO_ApplicationData{
					Type:      &appType,
					IpAddress: &ipAddress,
				}
				entityDto.ApplicationData = appData
				result = append(result, entityDto)
			}
		}
	}
	return
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

		// Now build entityDTO
		for serviceID, podIDList := range serviceEndpointMap {
			glog.Infof("service %s has the following pod as endpoints %s", serviceID, podIDList)

			if len(podIDList) < 1 {
				continue
			}

			processMap := pod2AppMap[podIDList[0]]
			for appName := range processMap {
				// first find out what processes are in this service
				serviceEntityType := sdk.EntityDTO_VIRTUAL_APPLICATION
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
