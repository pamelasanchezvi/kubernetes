package probe

import (
	"strconv"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client"

	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"

	vmtAdvisor "k8s.io/kubernetes/plugin/pkg/vmturbo/vmt/cadvisor"

	"github.com/golang/glog"
)

var hostSet map[string]*vmtAdvisor.Host = make(map[string]*vmtAdvisor.Host)

var nodeUidTranslationMap map[string]string = make(map[string]string)

var nodeName2ExternalIPMap map[string]string = make(map[string]string)

type NodeProbe struct {
	kubeClient  *client.Client
	nodesGetter NodesGetter
}

// Since this is only used in probe package, do not expose it.
func NewNodeProbe() *NodeProbe {
	return &NodeProbe{}
}

type NodesGetter func(label labels.Selector, field fields.Selector) []*api.Node

func (nodeProbe *NodeProbe) GetNodes(label labels.Selector, field fields.Selector) []*api.Node {
	//TODO check if nodesGetter is set

	return nodeProbe.nodesGetter(label, field)
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
