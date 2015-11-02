package vmt

import (
	"fmt"
	"strconv"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"

	"github.com/vmturbo/vmturbo-go-sdk/sdk"

	"github.com/google/cadvisor/info/v1"

	"github.com/golang/glog"
)

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
		cadvisor := &cadvisorSource{}
		host := &Host{
			IP:       nodeIP,
			Port:     4194,
			Resource: "",
		}
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
			return nil, fmt.Errorf("Not enough status data of current node %s.", nodeIP)
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
		id := node.Name
		dispName := node.Name
		entityDTOBuilder := sdk.NewEntityDTOBuilder(nodeEntityType, id)

		// Find out the used value for each commodity
		cpuUsed := float64(rootCurCpu) * float64(cpuFrequency)
		memUsed := float64(rootCurMem)

		//machineUid := node.Status.NodeInfo.MachineID

		// Build the entityDTO.
		entityDTOBuilder = entityDTOBuilder.DisplayName(dispName)
		entityDTOBuilder = entityDTOBuilder.Sells(sdk.CommodityDTO_MEM_ALLOCATION, "Container").
			Capacity(float64(nodeMemCapacity)).Used(memUsed)
		entityDTOBuilder = entityDTOBuilder.Sells(sdk.CommodityDTO_CPU_ALLOCATION, "Container").
			Capacity(float64(nodeCpuCapacity)).Used(cpuUsed)
		//entityDTOBuilder = entityDTOBuilder.setProvider(EntityDTOS_PhysicalMachine, machineUid)
		//entityDTOBuilder = entityDTOBuilder.buys(CommodityDTOS_CPU, "", cpuUsed)
		//entityDTOBuilder = entityDTOBuilder.buys(CommodityDTOS_Mem, "", memUsed)

		entityDto := entityDTOBuilder.Create()
		result = append(result, entityDto)
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
	podContainers := make(map[string][]*Container)

	// a pod to Node map
	podNodeMap := make(map[string]*v1.MachineInfo)

	// For each host there should be a cAdvisor client.
	for hostIP, _ := range nodePodsMap {

		// use cadvisor to get all containers on that host
		cadvisor := &cadvisorSource{}

		// TODO, must check the port number for cAdvisor client is static.
		host := &Host{
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
				if value, ok := container.Spec.Labels["io.kubernetes.pod.name"]; ok {
					glog.V(4).Infof("Container %s is in Pod %s", container.Name, value)
					var containers []*Container
					if ctns, exist := podContainers[value]; exist {
						containers = ctns
					}
					containers = append(containers, container)
					podContainers[value] = containers

					// Store in pod node map.
					if _, exist := podNodeMap[value]; !exist {
						podNodeMap[value] = machineInfo
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
					return nil, fmt.Errorf("Not enough data for %s", podNameWithNamespace)
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
		podEntityType := sdk.EntityDTO_CONTAINER
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
		entityDTOBuilder = entityDTOBuilder.Sells(sdk.CommodityDTO_MEM_ALLOCATION, "").Capacity(podMemCapacity).Used(podMemCapacity)
		entityDTOBuilder = entityDTOBuilder.Sells(sdk.CommodityDTO_CPU_ALLOCATION, "").Capacity(podCpuCapacity).Used(podCpuCapacity)
		entityDTOBuilder = entityDTOBuilder.SetProvider(sdk.EntityDTO_VIRTUAL_MACHINE, minionId)
		entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_CPU_ALLOCATION, "Container", podCpuCapacity)
		entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_MEM_ALLOCATION, "Container", podMemCapacity)

		entityDto := entityDTOBuilder.Create()
		result = append(result, entityDto)
	}

	for _, entityDto := range result {
		glog.V(3).Infof("Pod EntityDTO: " + entityDto.GetDisplayName())
	}

	return
}

// Show container stats for each container. For debug and troubleshooting purpose.
func showContainerStats(container *Container) {
	glog.V(3).Infof("Host name %s", container.Hostname)
	glog.V(3).Infof("Container name is %s", container.Name)
	containerStats := container.Stats
	currentStat := containerStats[len(containerStats)-1]
	glog.V(3).Infof("CPU usage is %d", currentStat.Cpu.Usage.Total)
	glog.V(3).Infof("MEM usage is %d", currentStat.Memory.Usage)
}
