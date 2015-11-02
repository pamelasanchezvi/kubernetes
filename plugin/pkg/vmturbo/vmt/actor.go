package vmt

import (
	"fmt"
	"strings"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"

	"k8s.io/kubernetes/plugin/pkg/vmturbo/vmt/registry"

	"github.com/vmturbo/vmturbo-go-sdk/sdk"

	"github.com/golang/glog"
)

// KubernetesActionExecutor is responsilbe for executing different kinds of action requested by vmt server.
type KubernetesActionExecutor struct {
	kubeClient *client.Client
}

// Switch between different kinds of the action according to action request and call the actually corresponding
// action execution method.
func (kae *KubernetesActionExecutor) ExcuteAction(actionItem *sdk.ActionItemDTO, msgID int32) error {
	if actionItem == nil {
		return fmt.Errorf("ActionItem sent in is null")
	}

	if actionItem.GetActionType() == sdk.ActionItemDTO_MOVE {
		// check if the targetSE is a Pod and the newSE is a VirtualMachine
		// TODO, for now, we use container to represent a pod
		glog.V(3).Infof("Now Move Pods.")
		if actionItem.GetTargetSE().GetEntityType() == sdk.EntityDTO_CONTAINER && actionItem.GetNewSE().GetEntityType() == sdk.EntityDTO_VIRTUAL_MACHINE {
			targetPod := actionItem.GetTargetSE()
			podIdentifier := targetPod.GetId()

			targetNode := actionItem.GetNewSE()
			nodeIdentifier := targetNode.GetId()
			// the pod identifier in vmt server is in "namespace/podname"
			content := strings.Split(string(podIdentifier), "/")
			if len(content) < 2 {
				return fmt.Errorf("Error getting pod name and namespace.")
			}
			podNamespace := content[0]
			podName := content[1]
			glog.V(3).Infof("Now Moving Pod %s in namespace %s.", podName, podNamespace)
			kae.MovePod(podName, podNamespace, nodeIdentifier, msgID)
		}
	}

	return nil
}

// Create new VMT Actor. Must specify the kubernetes client.
func NewKubeActor(client *client.Client) *KubernetesActionExecutor {
	return &KubernetesActionExecutor{
		kubeClient: client,
	}
}

// Move is such an action that should be excuted as first delete the particular pod
// then replication controller will create a new replica. The place to deploy the replica
// should be generated from vmt server.
func (this *KubernetesActionExecutor) MovePod(podIdentifier, namespace, targetNodeIdentifier string, msgID int32) (err error) {
	kubeClient := this.kubeClient

	glog.V(4).Infof("Now K8s trys to  move pod %s.\n", podIdentifier)
	// TODO !!!For test purpose, delete the first pod
	if podIdentifier == "" {
		return fmt.Errorf("Pod identifier should not be empty.\n")
	}
	if namespace == "" {
		glog.Warningf("Namespace is not specified. Use the default namespance.\n")
	}
	if targetNodeIdentifier == "" {
		glog.Warningf("Destination is not specified. Schedule to original host.\n")
	}
	err = kubeClient.Pods(namespace).Delete(podIdentifier, nil)
	if err != nil {
		glog.Errorf("Error when deleting pod %s: %s.\n", podIdentifier, err)
	} else {
		glog.V(3).Infof("Successfully delete pod %s.\n", podIdentifier)
	}

	// TODO! For now the move aciton is accomplished by the MoveSimulator.
	moveSimulator := &MoveSimulator{}
	action := "move"
	moveSimulator.SimulateMove(action, podIdentifier, targetNodeIdentifier, msgID)

	//------------------------------------------------------------------------------
	// The desired move scenario is using etcd.
	// Here it first post action event onto etcd. Then other component watches etcd will get the move event.
	vmtEvents := registry.NewVMTEvents(this.kubeClient, "")
	event := registry.GenerateVMTEvent(action, namespace, podIdentifier, targetNodeIdentifier, int(msgID))
	_, errorPost := vmtEvents.Create(event)
	if errorPost != nil {
		glog.Errorf("Error posting vmtevent: %s\n", errorPost)
	}

	return
}

// TODO. Thoughts. This is used to scale up and down. So we need the pod namespace and label here.
func (this *KubernetesActionExecutor) UpdateReplicas(podLabel, namespace string, newReplicas int) (err error) {
	replicationControllers, err := this.GetAllRC(namespace)
	if err != nil {
		return
	}
	if len(replicationControllers) < 1 {
		return
	}
	var targetRC api.ReplicationController
	for _, rc := range replicationControllers {
		labels := rc.Labels
		if labelName, ok := labels["name"]; ok && labelName == podLabel {
			targetRC = rc
		}
	}
	if &targetRC == nil {
		// TODO. Not sure here need error or just a warning.
		glog.Warning("This pod is not managed by any replication controllers")
		return fmt.Errorf("This pod is not managed by any replication controllers")
	}
	targetRC.Spec.Replicas = newReplicas

	kubeClient := this.kubeClient
	newRC, err := kubeClient.ReplicationControllers(namespace).Update(&targetRC)
	if err != nil {
		glog.V(3).Infof("New replicas of %s is %d", newRC.Name, newRC.Spec.Replicas)
	}
	return
}

// Get all replication controllers defined in the specified namespace.
func (this *KubernetesActionExecutor) GetAllRC(namespace string) (replicationControllers []api.ReplicationController, err error) {
	rcList, err := this.kubeClient.ReplicationControllers(namespace).List(labels.Everything())
	if err != nil {
		glog.Errorf("Error when getting all the replication controllers: %s", err)
	}
	replicationControllers = rcList.Items
	for _, rc := range replicationControllers {
		glog.V(4).Infof("Find replication controller: %s", rc.Name)
	}
	return
}

// Get all nodes currently in K8s.
func (this *KubernetesActionExecutor) GetAllNodes() []*api.Node {
	nodeList, err := this.kubeClient.Nodes().List(labels.Everything(), fields.Everything())
	if err != nil {
		return nil
	}
	glog.V(5).Infof("NodeList is %v ", nodeList)

	var nodeItems []*api.Node
	for _, node := range nodeList.Items {
		glog.V(3).Infof("Find a node %s ", node.Name)

		nodeItems = append(nodeItems, &node)
	}
	return nodeItems
}
