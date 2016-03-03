package probe

import (
	"fmt"
	"strconv"
	// "strings"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
	// "k8s.io/kubernetes/pkg/runtime"

	// vmtproxy "k8s.io/kubernetes/pkg/proxy/vmturbo"
	vmtAdvisor "k8s.io/kubernetes/plugin/pkg/vmturbo/vmt/cadvisor"
	// vmtmonitor "k8s.io/kubernetes/plugin/pkg/vmturbo/vmt/monitor"

	"github.com/golang/glog"
	// info "github.com/google/cadvisor/info/v2"
	"github.com/vmturbo/vmturbo-go-sdk/sdk"
)

var container2PodMap map[string]string = make(map[string]string)

var podHostIP2PodMap map[string]*api.Pod = make(map[string]*api.Pod)

// Pods Getter is such func that gets all the pods match the provided namespace, labels and fiels.
type PodsGetter func(namespace string, label labels.Selector, field fields.Selector) ([]*api.Pod, error)

type PodProbe struct {
	podGetter PodsGetter
}

func NewPodProbe(getter PodsGetter) *PodProbe {
	return &PodProbe{
		podGetter: getter,
	}
}

func (this *PodProbe) GetPods(namespace string, label labels.Selector, field fields.Selector) ([]*api.Pod, error) {
	if this.podGetter == nil {
		return nil, fmt.Errorf("Error. podGetter is not set.")
	}

	return this.podGetter(namespace, label, field)
}

type VMTPodGetter struct {
	kubeClient *client.Client
}

func NewVMTPodGetter(kubeClient *client.Client) *VMTPodGetter {
	return &VMTPodGetter{
		kubeClient: kubeClient,
	}
}

// Get pods match specified namesapce, label and field.
func (this *VMTPodGetter) GetPods(namespace string, label labels.Selector, field fields.Selector) ([]*api.Pod, error) {
	podList, err := this.kubeClient.Pods(namespace).List(label, field)
	if err != nil {
		return nil, fmt.Errorf("Error getting all the desired pods from Kubernetes cluster: %s", err)
	}
	var podItems []*api.Pod
	for _, pod := range podList.Items {
		p := pod
		hostIP := p.Status.HostIP
		podHostIP2PodMap[hostIP] = &p
		podItems = append(podItems, &p)
	}
	glog.V(3).Infof("Discovering Pods, now the cluster has " + strconv.Itoa(len(podItems)) + " pods")

	return podItems, nil
}

// Get pod resource usage from Kubernetes cluster and return a list of entityDTOs.
func (podProbe *PodProbe) parsePodFromK8s(pods []*api.Pod) (result []*sdk.EntityDTO, err error) {
	glog.V(3).Infof("Now getting pod metrics from cAdvisor")

	podContainers, err := podProbe.groupContainerByPod()
	if err != nil {
		return nil, err
	}

	glog.V(3).Infof("Now parse Pods")
	for _, pod := range pods {

		podResourceStat, err := podProbe.getPodResourceStat(pod, podContainers)
		if err != nil {
			continue
		}

		commoditiesSold := podProbe.getCommoditiesSold(pod, podResourceStat)
		commoditiesBought := podProbe.getCommoditiesBought(podResourceStat)

		entityDto, _ := podProbe.buildPodEntityDTO(pod, commoditiesSold, commoditiesBought)

		result = append(result, entityDto)
	}

	for _, entityDto := range result {
		glog.V(3).Infof("Pod EntityDTO: " + entityDto.GetDisplayName())
	}

	return
}

// Group containers according to the hosting pod.
// TODO. Here we also create a container2PodMap, used by processProbe.
func (podProbe *PodProbe) groupContainerByPod() (map[string][]*vmtAdvisor.Container, error) {
	podContainers := make(map[string][]*vmtAdvisor.Container)

	for _, host := range hostSet {
		// use cadvisor to get all containers on that host
		cadvisor := &vmtAdvisor.CadvisorSource{}

		// Only get subcontainers.
		subcontainers, _, err := cadvisor.GetAllContainers(*host, time.Now(), time.Now())
		if err != nil {
			return nil, err
		}

		// Map container to each pod. Key is pod name, value is container.
		for _, container := range subcontainers {
			spec := container.Spec
			if &spec != nil && container.Spec.Labels != nil {
				// TODO! hardcoded here. Works but not good. Maybe can find better solution?
				// The value returned here is namespace/podname
				if podName, ok := container.Spec.Labels["io.kubernetes.pod.name"]; ok {
					if podNamespace, hasNamespace := container.Spec.Labels["io.kubernetes.pod.namespace"]; hasNamespace {
						podName = podNamespace + "/" + podName
					}
					glog.V(4).Infof("Container %s is in Pod %s", container.Name, podName)
					var containers []*vmtAdvisor.Container
					if ctns, exist := podContainers[podName]; exist {
						containers = ctns
					}
					containers = append(containers, container)
					podContainers[podName] = containers

					// Map container to hosting pod. This map will be used in application probe.
					container2PodMap[container.Name] = podName
				}
			}
		}
	}

	return podContainers, nil
}

// Get the current pod resource capacity and usage.
func (podProbe *PodProbe) getPodResourceStat(pod *api.Pod, podContainers map[string][]*vmtAdvisor.Container) (*PodResourceStat, error) {
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

	// get cpu frequency
	cpuFrequency := nodeFrequencyMap[pod.Spec.NodeName]

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
		glog.Warningf("Cannot find pod %s", podNameWithNamespace)
		return nil, fmt.Errorf("Cannot find pod %s", podNameWithNamespace)
	}

	// convert num of core to frequecy in MHz
	podCpuUsed = podCpuUsed * float64(cpuFrequency)
	podMemUsed = podMemUsed / 1024 // Mem is in bytes, convert to Kb

	glog.V(4).Infof("The actual Cpu used value of %s is %f", podNameWithNamespace, podCpuUsed)
	glog.V(4).Infof("The actual Mem used value of %s is %f", podNameWithNamespace, podMemUsed)

	if actionTestingFlag {
		podCpuUsed = podCpuCapacity
		podMemUsed = podMemCapacity
	}

	glog.V(3).Infof(" Discovered pod is " + pod.Name)
	glog.V(4).Infof(" Pod %s CPU request is %f", pod.Name, podCpuUsed)
	glog.V(4).Infof(" Pod %s Mem request is %f", pod.Name, podMemUsed)

	return &PodResourceStat{
		cpuAllocationCapacity: podCpuCapacity,
		cpuAllocationUsed:     podCpuUsed,
		memAllocationCapacity: podMemCapacity,
		memAllocationUsed:     podMemUsed,
	}, nil
}

// Build commodityDTOs for commodity sold by the pod
func (podProbe *PodProbe) getCommoditiesSold(pod *api.Pod, podResourceStat *PodResourceStat) []*sdk.CommodityDTO {
	podNameWithNamespace := pod.Namespace + "/" + pod.Name
	var commoditiesSold []*sdk.CommodityDTO
	memAllocationComm := sdk.NewCommodtiyDTOBuilder(sdk.CommodityDTO_MEM_ALLOCATION).
		Key(podNameWithNamespace).
		Capacity(float64(podResourceStat.memAllocationCapacity)).
		Used(podResourceStat.memAllocationUsed).
		Create()
	commoditiesSold = append(commoditiesSold, memAllocationComm)
	cpuAllocationComm := sdk.NewCommodtiyDTOBuilder(sdk.CommodityDTO_CPU_ALLOCATION).
		Key(podNameWithNamespace).
		Capacity(float64(podResourceStat.cpuAllocationCapacity)).
		Used(podResourceStat.cpuAllocationUsed).
		Create()
	commoditiesSold = append(commoditiesSold, cpuAllocationComm)
	return commoditiesSold
}

// Build commodityDTOs for commodity sold by the pod
func (podProbe *PodProbe) getCommoditiesBought(podResourceStat *PodResourceStat) []*sdk.CommodityDTO {
	var commoditiesBought []*sdk.CommodityDTO
	cpuAllocationCommBought := sdk.NewCommodtiyDTOBuilder(sdk.CommodityDTO_CPU_ALLOCATION).
		Key("Container").
		Used(podResourceStat.cpuAllocationUsed).
		Create()
	commoditiesBought = append(commoditiesBought, cpuAllocationCommBought)
	memAllocationCommBought := sdk.NewCommodtiyDTOBuilder(sdk.CommodityDTO_MEM_ALLOCATION).
		Key("Container").
		Used(podResourceStat.memAllocationUsed).
		Create()
	commoditiesBought = append(commoditiesBought, memAllocationCommBought)
	return commoditiesBought
}

// Build entityDTO that contains all the necessary info of a pod.
func (podProbe *PodProbe) buildPodEntityDTO(pod *api.Pod, commoditiesSold, commoditiesBought []*sdk.CommodityDTO) (*sdk.EntityDTO, error) {
	podNameWithNamespace := pod.Namespace + "/" + pod.Name
	id := podNameWithNamespace
	dispName := podNameWithNamespace

	entityDTOBuilder := sdk.NewEntityDTOBuilder(sdk.EntityDTO_CONTAINER_POD, id)
	entityDTOBuilder.DisplayName(dispName)

	minionId := pod.Spec.NodeName
	if minionId == "" {
		return nil, fmt.Errorf("Cannot find the hosting node ID for pod %s", podNameWithNamespace)
	}
	glog.V(4).Infof("Pod %s is hosted on %s", dispName, minionId)

	entityDTOBuilder.SellsCommodities(commoditiesSold)
	providerUid := nodeUidTranslationMap[minionId]
	entityDTOBuilder = entityDTOBuilder.SetProvider(sdk.EntityDTO_VIRTUAL_MACHINE, providerUid)

	// entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_CPU_ALLOCATION, "Container", podCpuUsed)
	// entityDTOBuilder = entityDTOBuilder.Buys(sdk.CommodityDTO_MEM_ALLOCATION, "Container", podMemUsed)
	entityDTOBuilder.BuysCommodities(commoditiesBought)

	ipAddress := podProbe.getIPForStitching(pod)
	entityDTOBuilder = entityDTOBuilder.SetProperty("ipAddress", ipAddress)
	glog.V(3).Infof("Parse pod: The ip of vm to be stitched is %s", ipAddress)

	entityDto := entityDTOBuilder.Create()
	return entityDto, nil
}

// Get the IP address that will be used for stitching process.
// TODO, this needs to be consistent with the hypervisor probe.
// The correct behavior depends on what kind of IP address the hypervisor probe picks.
func (podProbe *PodProbe) getIPForStitching(pod *api.Pod) string {
	if localTestingFlag {
		return "10.10.173.196"
	}
	ipAddress := pod.Status.HostIP
	minionId := pod.Spec.NodeName
	if externalIP, ok := nodeName2ExternalIPMap[minionId]; ok {
		ipAddress = externalIP
	}
	return ipAddress
}
