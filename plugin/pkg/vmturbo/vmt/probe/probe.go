package probe

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
	// "k8s.io/kubernetes/pkg/runtime"

	vmtproxy "k8s.io/kubernetes/pkg/proxy/vmturbo"
	vmtAdvisor "k8s.io/kubernetes/plugin/pkg/vmturbo/vmt/cadvisor"
	vmtmonitor "k8s.io/kubernetes/plugin/pkg/vmturbo/vmt/monitor"

	"github.com/golang/glog"
	"github.com/google/cadvisor/info/v1"
	info "github.com/google/cadvisor/info/v2"
	"github.com/vmturbo/vmturbo-go-sdk/sdk"
)

var container2PodMap map[string]string = make(map[string]string)

var pod2AppMap map[string]map[string]vmtAdvisor.Application = make(map[string]map[string]vmtAdvisor.Application)

var podTransactionCountMap map[string]int = make(map[string]int)

var localTestingFlag bool = true

var actionTestingFlag bool = false

type KubeProbe struct {
	KubeClient *client.Client
	NodeProbe  *NodeProbe
}

// Create a new Kubernetes probe with the given kube client.
func NewKubeProbe(kubeClient *client.Client) *KubeProbe {
	vmtNodeGetter := NewVMTNodeGetter(kubeClient)
	nodeProbe := NewNodeProbe(vmtNodeGetter.GetNodes)
	return &KubeProbe{
		KubeClient: kubeClient,
		NodeProbe:  nodeProbe,
	}
}

func (this *KubeProbe) ParseNode() (result []*sdk.EntityDTO, err error) {
	k8sNodes := this.NodeProbe.GetNodes(labels.Everything(), fields.Everything())

	result, err = this.NodeProbe.parseNodeFromK8s(k8sNodes)
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
func (kubeProbe *KubeProbe) getAllPods(namespace string) map[string]*api.Pod {
	podItems := make(map[string]*api.Pod)

	podList, err := kubeProbe.KubeClient.Pods(namespace).List(labels.Everything(), fields.Everything())
	if err != nil {
		return nil
	}
	for _, pod := range podList.Items {
		p := pod
		podItems[pod.Status.PodIP] = &p
	}
	glog.V(3).Infof("Discovering Pods, now the cluster has " + strconv.Itoa(len(podItems)) + " pods")

	return podItems
}

func (kubeProbe *KubeProbe) parsePodFromK8s(pods map[string]*api.Pod) (result []*sdk.EntityDTO, err error) {
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

		if actionTestingFlag {
			podCpuUsed = podCpuCapacity
			podMemUsed = podMemCapacity
		}
		entityDTOBuilder = entityDTOBuilder.DisplayName(dispName)
		entityDTOBuilder = entityDTOBuilder.Sells(sdk.CommodityDTO_MEM_ALLOCATION, podNameWithNamespace).Capacity(podMemCapacity).Used(podMemUsed) // * 0.8)
		entityDTOBuilder = entityDTOBuilder.Sells(sdk.CommodityDTO_CPU_ALLOCATION, podNameWithNamespace).Capacity(podCpuCapacity).Used(podCpuUsed) // * 0.8)
		providerUid := nodeUidTranslationMap[minionId]
		entityDTOBuilder = entityDTOBuilder.SetProvider(sdk.EntityDTO_VIRTUAL_MACHINE, providerUid)
		entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_CPU_ALLOCATION, "Container", podCpuUsed)
		entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_MEM_ALLOCATION, "Container", podMemUsed)
		// entityDTOBuilder = entityDTOBuilder.SetProperty("ipAddress", "10.10.173.131")

		// entityDTOBuilder = entityDTOBuilder.SetProperty("ipAddress", "10.10.173.131")
		tmp := pod.Status.HostIP
		if localTestingFlag {
			tmp = "10.10.173.131"
		}
		ipAddress := tmp
		if externalIP, ok := nodeName2ExternalIPMap[minionId]; ok {
			ipAddress = externalIP
		}
		entityDTOBuilder = entityDTOBuilder.SetProperty("ipAddress", ipAddress)
		glog.V(3).Infof("Parse pod: The ip of vm to be stitched is %s", ipAddress)

		entityDto := entityDTOBuilder.Create()
		result = append(result, entityDto)
	}

	for _, entityDto := range result {
		glog.V(3).Infof("Pod EntityDTO: " + entityDto.GetDisplayName())
	}

	return
}

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

		transactionsCount, _ := kubeProbe.retrieveTransactoins()
		glog.Infof("transactions are: %s", transactionsCount)

		podSet := kubeProbe.getAllPods(namespace)
		for podIPAndPort, count := range transactionsCount {
			tempArray := strings.Split(podIPAndPort, ":")
			if len(tempArray) < 2 {
				continue
			}
			podIP := tempArray[0]
			pod, ok := podSet[podIP]
			if !ok {
				glog.Errorf("Cannot link pod with IP %s in the podSet", podIP)
				continue
			}
			podNameWithNamespace := pod.Namespace + "/" + pod.Name
			podTransactionCountMap[podNameWithNamespace] = count
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

				transactionCapacity := float64(1000)
				transactionUsed := float64(0)

				if count, ok := podTransactionCountMap[podName]; ok {
					transactionUsed = float64(count)
					glog.V(3).Infof("Get transactions value of pod %s, is %f", podName, transactionUsed)

				}

				if actionTestingFlag {
					transactionCapacity = float64(10000)
					transactionUsed = float64(9999)
				}

				entityDTOBuilder = entityDTOBuilder.DisplayName(dispName)
				entityDTOBuilder = entityDTOBuilder.Sells(sdk.CommodityDTO_TRANSACTION, app.Cmd).Capacity(transactionCapacity).Used(transactionUsed)
				entityDTOBuilder = entityDTOBuilder.SetProvider(sdk.EntityDTO_CONTAINER_POD, podName)
				entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_CPU_ALLOCATION, podName, cpuUsage)
				entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_MEM_ALLOCATION, podName, memUsage)

				providerUid := nodeUidTranslationMap[nodeName]
				entityDTOBuilder = entityDTOBuilder.SetProvider(sdk.EntityDTO_VIRTUAL_MACHINE, providerUid)
				entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_VCPU, providerUid, cpuUsage)
				entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_VMEM, providerUid, memUsage)
				appComm := sdk.NewCommodtiyDTOBuilder(sdk.CommodityDTO_APPLICATION).
					Key(providerUid).
					Create()
				entityDTOBuilder = entityDTOBuilder.BuysCommodity(appComm)
				// entityDTOBuilder = entityDTOBuilder.SetProperty("ipAddress", "10.10.173.131")

				entityDto := entityDTOBuilder.Create()

				appType := app.Cmd
				tmp := host.IP
				if localTestingFlag {
					tmp = "10.10.173.131"
				}
				ipAddress := tmp
				if externalIP, ok := nodeName2ExternalIPMap[nodeName]; ok {
					ipAddress = externalIP
				}
				glog.V(4).Infof("Parse application: The ip of vm to be stitched is %s", ipAddress)

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
	serviceList, err := kubeProbe.KubeClient.Services(namespace).List(selector)
	if err != nil {
		return nil, fmt.Errorf("Error listing services: %s", err)
	}

	endpointList, err := kubeProbe.KubeClient.Endpoints(namespace).List(selector)
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
					transactionBought := float64(0)

					if count, ok := podTransactionCountMap[podID]; ok {
						transactionBought = float64(count)
						glog.V(3).Infof("Transaction bought from pod %s is %f", podID, count)
					}

					if actionTestingFlag {
						transactionBought = float64(9999)
					}
					entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_TRANSACTION, appName, transactionBought)

				}

				entityDto := entityDTOBuilder.Create()

				glog.V(4).Infof("created a service entityDTO %v", entityDto)
				result = append(result, entityDto)
			}
		}
	}

	return
}

func (this *KubeProbe) retrieveTransactoins() (map[string]int, error) {
	servicesTransactions, err := this.getTransactionFromAllNodes()
	if err != nil {
		return nil, err
	}

	ep2TransactionCountMap := make(map[string]int)
	for _, transaction := range servicesTransactions {
		epCounterMap := transaction.GetEndpointsCounterMap()
		for ep, count := range epCounterMap {
			curCount, exist := ep2TransactionCountMap[ep]
			if !exist {
				curCount = 0
			}
			curCount = curCount + count
			ep2TransactionCountMap[ep] = curCount
		}
	}
	return ep2TransactionCountMap, nil
}

func (this *KubeProbe) getTransactionFromAllNodes() (transactionInfo []vmtproxy.Transaction, err error) {
	for nodeName, host := range hostSet {
		transactions, err := this.getTransactionFromNode(host)
		if err != nil {
			glog.Errorf("error: %s", err)
			// TODO, do not return in order to not block the discover in other host.
			continue
		}
		if len(transactions) < 1 {
			glog.Warningf("No transaction data in %s.", nodeName)
			continue
		}
		glog.Infof("Transaction from %s is: %v", nodeName, transactions)

		transactionInfo = append(transactionInfo, transactions...)
	}
	return transactionInfo, nil
}

func (this *KubeProbe) getTransactionFromNode(host *vmtAdvisor.Host) ([]vmtproxy.Transaction, error) {
	glog.V(4).Infof("Now get transactions in host %s", host.IP)
	monitor := &vmtmonitor.ServiceMonitor{}
	transactions, err := monitor.GetServiceTransactions(*host)
	if err != nil {
		glog.Errorf("Error getting transaction data from %s: %s", host.IP, err)
		return transactions, err
	}
	return transactions, nil
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
