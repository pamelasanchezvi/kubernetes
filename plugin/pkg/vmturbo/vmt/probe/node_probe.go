package probe

import (
	"fmt"
	"strconv"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client"

	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"

	vmtAdvisor "k8s.io/kubernetes/plugin/pkg/vmturbo/vmt/cadvisor"

	"github.com/vmturbo/vmturbo-go-sdk/sdk"

	"github.com/golang/glog"
)

var hostSet map[string]*vmtAdvisor.Host = make(map[string]*vmtAdvisor.Host)

var nodeUidTranslationMap map[string]string = make(map[string]string)

var nodeName2ExternalIPMap map[string]string = make(map[string]string)

type NodeProbe struct {
	nodesGetter NodesGetter
	nodeIPMap   map[string]map[api.NodeAddressType]string
}

// Since this is only used in probe package, do not expose it.
func NewNodeProbe(getter NodesGetter) *NodeProbe {
	return &NodeProbe{
		nodesGetter: getter,
	}
}

type VMTNodeGetter struct {
	kubeClient *client.Client
}

func NewVMTNodeGetter(kubeClient *client.Client) *VMTNodeGetter {
	return &VMTNodeGetter{
		kubeClient: kubeClient,
	}
}

// Get all nodes
func (this *VMTNodeGetter) GetNodes(label labels.Selector, field fields.Selector) []*api.Node {
	nodeList, err := this.kubeClient.Nodes().List(label, field)
	if err != nil {
		//TODO error must be handled
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

type NodesGetter func(label labels.Selector, field fields.Selector) []*api.Node

func (nodeProbe *NodeProbe) GetNodes(label labels.Selector, field fields.Selector) []*api.Node {
	//TODO check if nodesGetter is set

	return nodeProbe.nodesGetter(label, field)
}

// Parse each node inside K8s. Get the resources usage of each node and build the entityDTO.
func (nodeProbe *NodeProbe) parseNodeFromK8s(nodes []*api.Node) (result []*sdk.EntityDTO, err error) {
	glog.V(3).Infof("---------- Now parse Node ----------")

	for _, node := range nodes {
		// First, use cAdvisor to get node info
		//machineId := node.Status.NodeInfo.MachineID
		nodeProbe.parseNodeIP(node)
		hostSet[node.Name] = nodeProbe.getHost(node.Name)

		nodeResourceStat := nodeProbe.getNodeResourceStat(node)

		// // Now start to build supply chain.
		// nodeEntityType := sdk.EntityDTO_VIRTUAL_MACHINE
		nodeID := string(node.UID)
		dispName := node.Name
		nodeUidTranslationMap[node.Name] = nodeID
		// entityDTOBuilder := sdk.NewEntityDTOBuilder(nodeEntityType, id)

		var commoditiesSold []*sdk.CommodityDTO
		//TODO: create const value for keys
		memAllocationComm := sdk.NewCommodtiyDTOBuilder(sdk.CommodityDTO_MEM_ALLOCATION).
			Key("Container").
			Capacity(float64(nodeResourceStat.memAllocationCapacity)).
			Used(nodeResourceStat.memAllocationUsed).
			Create()
		commoditiesSold = append(commoditiesSold, memAllocationComm)
		cpuAllocationComm := sdk.NewCommodtiyDTOBuilder(sdk.CommodityDTO_CPU_ALLOCATION).
			Key("Container").
			Capacity(float64(nodeResourceStat.cpuAllocationCapacity)).
			Used(nodeResourceStat.cpuAllocationUsed).
			Create()
		commoditiesSold = append(commoditiesSold, cpuAllocationComm)
		vMemComm := sdk.NewCommodtiyDTOBuilder(sdk.CommodityDTO_VMEM).
			Key(nodeID).
			Capacity(nodeResourceStat.vMemCapacity).
			Used(nodeResourceStat.vMemUsed).
			Create()
		commoditiesSold = append(commoditiesSold, vMemComm)
		vCpuComm := sdk.NewCommodtiyDTOBuilder(sdk.CommodityDTO_VCPU).
			Key(nodeID).
			Capacity(float64(nodeResourceStat.vCpuCapacity)).
			Used(nodeResourceStat.vCpuUsed).
			Create()
		commoditiesSold = append(commoditiesSold, vCpuComm)
		appComm := sdk.NewCommodtiyDTOBuilder(sdk.CommodityDTO_APPLICATION).
			Key(nodeID).
			Create()
		commoditiesSold = append(commoditiesSold, appComm)

		entityDto := nodeProbe.buildVMEntityDTO(nodeID, dispName, commoditiesSold)

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

func (nodeProbe *NodeProbe) getHost(nodeName string) *vmtAdvisor.Host {
	nodeIP, err := nodeProbe.getNodeIPWithType(nodeName, api.NodeLegacyHostIP)
	if err != nil {
		glog.Errorf("Error getting NodeLegacyHostIP of node %s: %s.", nodeName, err)
		return nil
	}
	// Use NodeLegacyHostIP to build the host to interact with cAdvisor.
	host := &vmtAdvisor.Host{
		IP:       nodeIP,
		Port:     4194,
		Resource: "",
	}
	return host
}

// func (this *NodeProbe) getNodeUidFromName(nodeName string) string {

// }

// Retrieve the legacyHostIP of each node and put other IPs to related maps.
func (nodeProbe *NodeProbe) parseNodeIP(node *api.Node) {
	if nodeProbe.nodeIPMap == nil {
		nodeProbe.nodeIPMap = make(map[string]map[api.NodeAddressType]string)
	}
	currentNodeIPMap := make(map[api.NodeAddressType]string)

	nodeAddresses := node.Status.Addresses
	for _, nodeAddress := range nodeAddresses {
		if nodeAddress.Type == api.NodeExternalIP {
			nodeName2ExternalIPMap[node.Name] = nodeAddress.Address
		}
		currentNodeIPMap[nodeAddress.Type] = nodeAddress.Address
	}

	nodeProbe.nodeIPMap[node.Name] = currentNodeIPMap
}

func (this *NodeProbe) getNodeIPWithType(nodeName string, ipType api.NodeAddressType) (string, error) {
	ips, ok := this.nodeIPMap[nodeName]
	if !ok {
		return "", fmt.Errorf("IP of node %s is not tracked", nodeName)
	}
	nodeIP, exist := ips[ipType]
	if !exist {
		return "", fmt.Errorf("node %s does not have %s.", nodeName, ipType)
	}
	return nodeIP, nil
}

func (nodeProbe *NodeProbe) buildVMEntityDTO(nodeID, displayName string, commoditiesSold []*sdk.CommodityDTO) *sdk.EntityDTO {
	entityDTOBuilder := sdk.NewEntityDTOBuilder(sdk.EntityDTO_VIRTUAL_MACHINE, nodeID)
	entityDTOBuilder.DisplayName(displayName)
	entityDTOBuilder.SellsCommodities(commoditiesSold)

	ipAddress := nodeProbe.getIPForStitching(displayName)
	entityDTOBuilder = entityDTOBuilder.SetProperty("IP", ipAddress)
	glog.V(4).Infof("Parse node: The ip of vm to be reconcile with is %s", ipAddress)

	metaData := nodeProbe.generateReconcilationMetaData()

	entityDTOBuilder = entityDTOBuilder.ReplacedBy(metaData)

	entityDto := entityDTOBuilder.Create()

	return entityDto
}

// Create the meta data that will be used during the reconcilation process.
func (nodeProbe *NodeProbe) generateReconcilationMetaData() *sdk.EntityDTO_ReplacementEntityMetaData {
	replacementEntityMetaDataBuilder := sdk.NewReplacementEntityMetaDataBuilder()
	replacementEntityMetaDataBuilder.Matching("IP")
	replacementEntityMetaDataBuilder.PatchSelling(sdk.CommodityDTO_CPU_ALLOCATION)
	replacementEntityMetaDataBuilder.PatchSelling(sdk.CommodityDTO_MEM_ALLOCATION)
	replacementEntityMetaDataBuilder.PatchSelling(sdk.CommodityDTO_VCPU)
	replacementEntityMetaDataBuilder.PatchSelling(sdk.CommodityDTO_VMEM)
	replacementEntityMetaDataBuilder.PatchSelling(sdk.CommodityDTO_APPLICATION)
	// replacementEntityMetaDataBuilder.PatchBuying(sdk.CommodityDTO_VCPU)
	// replacementEntityMetaDataBuilder.PatchBuying(sdk.CommodityDTO_VMEM)

	metaData := replacementEntityMetaDataBuilder.Build()
	return metaData
}

// Get the correct IP that will be used during the stitching process.
func (nodeProbe *NodeProbe) getIPForStitching(nodeName string) string {
	if localTestingFlag {
		return "10.10.173.131"
	}
	nodeIPs, exist := nodeProbe.nodeIPMap[nodeName]
	if !exist {
		glog.Fatalf("Error trying to get IP of node %s in Kubernetes cluster", nodeName)
	}
	if externalIP, ok := nodeIPs[api.NodeExternalIP]; ok {
		return externalIP
	}

	return nodeIPs[api.NodeLegacyHostIP]
}

// Get current stat of node resources, such as capacity and used values.
func (this *NodeProbe) getNodeResourceStat(node *api.Node) *NodeResourceStat {
	cadvisor := &vmtAdvisor.CadvisorSource{}

	host := this.getHost(node.Name)
	machineInfo, err := cadvisor.GetMachineInfo(*host)
	if err != nil {
		return nil
		// return nil, err
	}
	// The return cpu frequency is in KHz, we need MHz
	cpuFrequency := machineInfo.CpuFrequency / 1000

	// Here we only need the root container.
	_, root, err := cadvisor.GetAllContainers(*host, time.Now(), time.Now())
	if err != nil {
		// return nil, err
	}
	containerStats := root.Stats
	// To get a valid cpu usage, there must be at least 2 valid stats.
	if len(containerStats) < 2 {
		glog.Warning("Not enough data")
		return nil
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
	// Find out the used value for each commodity
	cpuUsed := float64(rootCurCpu) * float64(cpuFrequency)
	memUsed := float64(rootCurMem)

	// this flag is defined at package level, in probe.go
	if localTestingFlag {
		cpuUsed = float64(10000)
	}

	return &NodeResourceStat{
		cpuAllocationCapacity: nodeCpuCapacity,
		cpuAllocationUsed:     cpuUsed,
		memAllocationCapacity: nodeMemCapacity,
		memAllocationUsed:     memUsed,
		vCpuCapacity:          nodeCpuCapacity,
		vCpuUsed:              cpuUsed,
		vMemCapacity:          nodeMemCapacity,
		vMemUsed:              memUsed,
	}
}

// For testing purpose, create fake vm entityDTO
func (this *NodeProbe) buildFakeVMEntityDTO() *sdk.EntityDTO {
	nodeEntityType := sdk.EntityDTO_VIRTUAL_MACHINE

	// create a fake VM
	entityDTOBuilder2 := sdk.NewEntityDTOBuilder(nodeEntityType, "1.1.1.1")
	// Find out the used value for each commodity
	cpuUsed := float64(0)
	memUsed := float64(0)
	nodeMemCapacity := float64(1000)
	nodeCpuCapacity := float64(1000)
	// Build the entityDTO.
	entityDTOBuilder2 = entityDTOBuilder2.DisplayName("1.1.1.1")
	var commodityDTOs []*sdk.CommodityDTO
	memAllocationComm := sdk.NewCommodtiyDTOBuilder(sdk.CommodityDTO_MEM_ALLOCATION).
		Key("Container").
		Capacity(float64(nodeMemCapacity)).
		Used(memUsed).
		Create()
	commodityDTOs = append(commodityDTOs, memAllocationComm)
	entityDTOBuilder2 = entityDTOBuilder2.Sells(sdk.CommodityDTO_MEM_ALLOCATION, "Container").
		Capacity(float64(nodeMemCapacity)).Used(memUsed)
	entityDTOBuilder2 = entityDTOBuilder2.Sells(sdk.CommodityDTO_CPU_ALLOCATION, "Container").
		Capacity(float64(nodeCpuCapacity)).Used(cpuUsed)
	entityDTOBuilder2 = entityDTOBuilder2.Sells(sdk.CommodityDTO_VMEM, "1.1.1.1").
		Capacity(float64(nodeMemCapacity)).Used(memUsed)
	entityDTOBuilder2 = entityDTOBuilder2.Sells(sdk.CommodityDTO_VCPU, "1.1.1.1").
		Capacity(float64(nodeCpuCapacity)).Used(cpuUsed)
	entityDTOBuilder2 = entityDTOBuilder2.SetProperty("IP", "10.10.173.196")

	metaData2 := this.generateReconcilationMetaData()

	entityDTOBuilder2 = entityDTOBuilder2.ReplacedBy(metaData2)
	entityDto2 := entityDTOBuilder2.Create()

	return entityDto2
}
