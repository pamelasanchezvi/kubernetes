package probe

import (
	"fmt"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"

	// vmtproxy "k8s.io/kubernetes/pkg/proxy/vmturbo"
	vmtAdvisor "k8s.io/kubernetes/plugin/pkg/vmturbo/vmt/cadvisor"
	// vmtmonitor "k8s.io/kubernetes/plugin/pkg/vmturbo/vmt/monitor"

	"github.com/golang/glog"
	"github.com/vmturbo/vmturbo-go-sdk/sdk"
)

var localTestingFlag bool = false

var actionTestingFlag bool = false

type KubeProbe struct {
	KubeClient *client.Client
	NodeProbe  *NodeProbe
	PodProbe   *PodProbe
	AppProbe   *ApplicationProbe
}

// Create a new Kubernetes probe with the given kube client.
func NewKubeProbe(kubeClient *client.Client) *KubeProbe {
	vmtNodeGetter := NewVMTNodeGetter(kubeClient)
	nodeProbe := NewNodeProbe(vmtNodeGetter.GetNodes)

	vmtPodGetter := NewVMTPodGetter(kubeClient)
	podProbe := NewPodProbe(vmtPodGetter.GetPods)

	applicationProbe := NewApplicationProbe()
	return &KubeProbe{
		KubeClient: kubeClient,
		NodeProbe:  nodeProbe,
		PodProbe:   podProbe,
		AppProbe:   applicationProbe,
	}
}

func (this *KubeProbe) ParseNode() (result []*sdk.EntityDTO, err error) {
	k8sNodes := this.NodeProbe.GetNodes(labels.Everything(), fields.Everything())

	result, err = this.NodeProbe.parseNodeFromK8s(k8sNodes)
	return
}

// Parse pods those are defined in namespace.
func (this *KubeProbe) ParsePod(namespace string) (result []*sdk.EntityDTO, err error) {
	k8sPods, err := this.PodProbe.GetPods(namespace, labels.Everything(), fields.Everything())
	if err != nil {
		return nil, err
	}

	result, err = this.PodProbe.parsePodFromK8s(k8sPods)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (this *KubeProbe) ParseApplication(namespace string) (result []*sdk.EntityDTO, err error) {
	return this.AppProbe.ParseApplication(namespace)
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
					entityDTOBuilder = entityDTOBuilder.SetProviderWithTypeAndID(sdk.EntityDTO_CONTAINER_POD, appName+"::"+podID)
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

// Show container stats for each container. For debug and troubleshooting purpose.
func showContainerStats(container *vmtAdvisor.Container) {
	glog.V(3).Infof("Host name %s", container.Hostname)
	glog.V(3).Infof("Container name is %s", container.Name)
	containerStats := container.Stats
	currentStat := containerStats[len(containerStats)-1]
	glog.V(3).Infof("CPU usage is %d", currentStat.Cpu.Usage.Total)
	glog.V(3).Infof("MEM usage is %d", currentStat.Memory.Usage)
}
