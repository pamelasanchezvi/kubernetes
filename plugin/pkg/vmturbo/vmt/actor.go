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
		if actionItem.GetTargetSE().GetEntityType() == sdk.EntityDTO_CONTAINER_POD && actionItem.GetNewSE().GetEntityType() == sdk.EntityDTO_VIRTUAL_MACHINE {
			targetPod := actionItem.GetTargetSE()
			podIdentifier := targetPod.GetId()

			targetNode := actionItem.GetNewSE()
			// nodeIdentifier := targetNode.GetId()
			// K8s uses Ip address as the Identifier. The VM name passed by actionItem is the display name
			// that dscovered by hypervisor. So here we must get the ip address from virtualMachineData in
			// targetNode entityDTO.
			vmData := targetNode.GetVirtualMachineData()
			if vmData == nil {
				return fmt.Errorf("Missing VirtualMachineData in ActionItemDTO from server")
			}
			machineIPs := vmData.GetIpAddress()
			glog.Infof("The ip of targetNode is %v", machineIPs)
			if machineIPs == nil {
				return fmt.Errorf("Missing Ip addresses in ActionItemDTO from server")
			}
			// TODO, must find a way to validate IP address.
			nodeIdentifier := machineIPs[0]

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
	} else if actionItem.GetActionType() == sdk.ActionItemDTO_PROVISION {
		glog.V(3).Infof("Now Provision Pods")
		if actionItem.GetTargetSE().GetEntityType() == sdk.EntityDTO_CONTAINER_POD {
			targetPod := actionItem.GetTargetSE()
			podIdentifier := targetPod.GetId()

			// find related replication controller through identifier.
			podIds := strings.Split(string(podIdentifier), "/")
			if len(podIds) != 2 {
				return fmt.Errorf("Not a valid pod identifier: %s", podIdentifier)
			}

			podNamespace := podIds[0]
			podNames := strings.Split(string(podIds[1]), "-")
			if len(podNames) != 2 {
				return fmt.Errorf("Cannot parse pod with name: %s", podIds[1])
			}
			replicationControllerName := podNames[0]
			targetReplicationController, err := kae.getReplicationController(replicationControllerName, podNamespace)
			if err != nil {
				return fmt.Errorf("Error getting replication controller related to pod %s: %s", podIdentifier, err)
			}
			if &targetReplicationController == nil {
				return fmt.Errorf("No replication controller defined with pod %s", podIdentifier)
			}
			currentReplica := targetReplicationController.Spec.Replicas
			err = kae.ProvisionPods(targetReplicationController, currentReplica+1, msgID)
			if err != nil {
				return fmt.Errorf("Error provision pod %s: %s", podIdentifier, err)
			}
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

	// if targetNodeIdentifier == "1.1.1.1" {
	// 	targetNodeIdentifier = "127.0.0.1"
	// }

	// TODO! For now the move aciton is accomplished by the MoveSimulator.
	moveSimulator := &MoveSimulator{}
	action := "move"
	moveSimulator.SimulateMove(action, podIdentifier, targetNodeIdentifier, msgID)

	//------------------------------------------------------------------------------
	// The desired move scenario is using etcd.
	// Here it first post action event onto etcd. Then other component watches etcd will get the move event.
	vmtEvents := registry.NewVMTEvents(this.kubeClient, "")
	event := registry.GenerateVMTEvent(action, namespace, podIdentifier, targetNodeIdentifier, int(msgID))
	glog.V(3).Infof("vmt event is %v, msgId is %d, %d", event, msgID, int(msgID))
	_, errorPost := vmtEvents.Create(event)
	if errorPost != nil {
		glog.Errorf("Error posting vmtevent: %s\n", errorPost)
	}

	return
}

// TODO. Thoughts. This is used to scale up and down. So we need the pod namespace and label here.
func (this *KubernetesActionExecutor) UpdateReplicas(podLabel, namespace string, newReplicas int) (err error) {
	targetRC, err := this.getReplicationController(podLabel, namespace)
	if err != nil {
		return fmt.Errorf("Error getting replication controller: %s", err)
	}
	if &targetRC == nil {
		// TODO. Not sure here need error or just a warning.
		glog.Warning("This pod is not managed by any replication controllers")
		return fmt.Errorf("This pod is not managed by any replication controllers")
	}
	err = this.ProvisionPods(targetRC, newReplicas, -1)
	return err
}

// Update replica of the target replication controller.
func (this *KubernetesActionExecutor) ProvisionPods(targetReplicationController api.ReplicationController, newReplicas int, msgID int32) (err error) {
	targetReplicationController.Spec.Replicas = newReplicas
	namespace := targetReplicationController.Namespace
	kubeClient := this.kubeClient
	newRC, err := kubeClient.ReplicationControllers(namespace).Update(&targetReplicationController)
	if err != nil {
		return fmt.Errorf("Error updating replication controller %s: %s", targetReplicationController.Name, err)
	}
	glog.V(3).Infof("New replicas of %s is %d", newRC.Name, newRC.Spec.Replicas)

	if msgID < 0 {
		return
	}

	action := "provision"
	vmtEvents := registry.NewVMTEvents(this.kubeClient, "")
	event := registry.GenerateVMTEvent(action, namespace, newRC.Name, "not specified", int(msgID))
	glog.V(3).Infof("vmt event is %v, msgId is %d, %d", event, msgID, int(msgID))
	_, errorPost := vmtEvents.Create(event)
	if errorPost != nil {
		glog.Errorf("Error posting vmtevent: %s\n", errorPost)
	}
	return
}

// Get the replication controller instance according to the name and namespace.
func (this *KubernetesActionExecutor) getReplicationController(rcName, namespace string) (api.ReplicationController, error) {
	var targetRC api.ReplicationController

	replicationControllers, err := this.GetAllRC(namespace)
	if err != nil {
		return targetRC, err
	}
	if len(replicationControllers) < 1 {
		return targetRC, fmt.Errorf("There is no replication controller defined in current cluster")
	}
	for _, rc := range replicationControllers {
		if rcName == rc.Name {
			targetRC = rc
		}
	}
	return targetRC, nil
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
