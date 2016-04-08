package vmturbo

import (
	"fmt"
	"strings"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/util"
	"k8s.io/kubernetes/plugin/pkg/scheduler"

	// "k8s.io/kubernetes/plugin/pkg/vmturbo/action"
	// vmtapi "k8s.io/kubernetes/plugin/pkg/vmturbo/api"
	"k8s.io/kubernetes/plugin/pkg/vmturbo/deploy"
	"k8s.io/kubernetes/plugin/pkg/vmturbo/registry"
	comm "k8s.io/kubernetes/plugin/pkg/vmturbo/vmturbocommunicator"

	"github.com/vmturbo/vmturbo-go-sdk/sdk"

	"github.com/golang/glog"
)

type VMTurboService struct {
	config       *Config
	vmtcomm      *comm.VMTCommunicator
	vmtEventChan chan *registry.VMTEvent
	// VMTurbo Scheduler
	vmtScheduler *deploy.VMTScheduler
}

func NewVMTurboService(c *Config) *VMTurboService {
	vmtService := &VMTurboService{
		config: c,
	}
	vmtService.vmtEventChan = make(chan *registry.VMTEvent)
	vmtService.vmtScheduler = deploy.NewVMTScheduler(vmtService.config.Client)
	return vmtService
}

// Run begins watching and scheduling. It starts a goroutine and returns immediately.
func (v *VMTurboService) Run() {
	glog.V(3).Infof("********** Start runnning VMT service **********")

	vmtCommunicator := comm.NewVMTCommunicator(v.config.Client, v.config.Meta, v.config.EtcdStorage)
	v.vmtcomm = vmtCommunicator
	// register and validates to vmturbo server
	go vmtCommunicator.Run()

	// for test vmtevents etcd registry
	vmtEvents := registry.NewVMTEvents(v.config.Client, "", v.config.EtcdStorage)
	event := registry.GenerateVMTEvent("create", "default", "podname", "1.0.0.0", 1)
	_, errorPost := vmtEvents.Create(event)
	if errorPost != nil {
		glog.Errorf("Error posting vmtevent: %s\n", errorPost)
	}

	//delete all the vmt events
	// vmtEvents := registry.NewVMTEvents(v.config.Client, "", v.config.EtcdStorage)
	errorDelete := vmtEvents.DeleteAll()
	if errorDelete != nil {
		glog.V(3).Infof("Error deleting all vmt events: %s", errorDelete)
	}

	// These three go routine is responsible for watching corresponding watchable resource.
	go util.Until(v.getNextNode, 0, v.config.StopEverything)
	go util.Until(v.getNextPod, 0, v.config.StopEverything)
	go util.Until(v.getNextVMTEvent, 0, v.config.StopEverything)
}

func (v *VMTurboService) getNextVMTEvent() {
	event := v.config.VMTEventQueue.Pop().(*registry.VMTEvent)
	glog.V(2).Infof("Get a new VMTEvent from etcd: %v", event)
	if event.ActionType == "move" || event.ActionType == "provision" {
		glog.V(2).Infof("VMTEvent must be handled.")
		// Send VMTEvent to channel.
		v.vmtEventChan <- event
	}
}

// When a new node is added in, this function is called. Otherwise, it is blocked.
func (v *VMTurboService) getNextNode() {
	node := v.config.NodeQueue.Pop().(*api.Node)
	glog.V(2).Infof("Get a new Node %v", node.Name)
}

// Whenever there is a new pod created and post to etcd, this method will be used to deal with
// unhandled pod. Otherwise it will block at Pop()
func (v *VMTurboService) getNextPod() {
	pod := v.config.PodQueue.Pop().(*api.Pod)
	glog.V(2).Infof("Get a new Pod %v", pod.Name)

	// If we want to track new Pod, create a VMTEvent and post it to etcd.
	vmtEvents := registry.NewVMTEvents(v.config.Client, "", v.config.EtcdStorage)
	event := registry.GenerateVMTEvent("create", pod.Namespace, pod.Name, "", 1)
	_, errorPost := vmtEvents.Create(event)
	if errorPost != nil {
		glog.Errorf("Error posting vmtevent: %s\n", errorPost)
	}

	// ----------------- try channel and vmtevent -------------
	select {
	case vmtEventFromEtcd := <-v.vmtEventChan:
		switch vmtEventFromEtcd.ActionType {
		case "move":
			glog.V(2).Infof("Pod %s/%s is to be scheduled to %s as a result of MOVE action",
				pod.Namespace, pod.Name, vmtEventFromEtcd.Destination)

			v.vmtScheduler.VMTSchedule(pod, vmtEventFromEtcd.Destination)

			break
		case "provision":
			glog.V(3).Infof("Change replicas of %s.", vmtEventFromEtcd.TargetSE)

			// double check if the pod is created as the result of provision
			hasPrefix := strings.HasPrefix(pod.Name, vmtEventFromEtcd.TargetSE)
			if !hasPrefix {
				break
			}
			v.schedule(pod)
		}
		// TODO, at this point, we really do not know if the assignment of the pod succeeds or not.
		// The right place of sending move reponse is after the event.
		// Here for test purpose, send the move success action response.
		if vmtEventFromEtcd.VMTMessageID > -1 {
			glog.V(3).Infof("Send action response to VMTurbo server.")
			progress := int32(100)
			v.vmtcomm.SendActionReponse(sdk.ActionResponseState_SUCCEEDED, progress, int32(vmtEventFromEtcd.VMTMessageID), "Success")
		}
		v.vmtcomm.DiscoverTarget()
		return
	default:
		glog.V(3).Infof("No VMTEvent from ETCD. Simply schedule the pod.")
	}
	v.schedule(pod)
}

func (vmtService *VMTurboService) schedule(pod *api.Pod) {
	var placementMap map[*api.Pod]string
	placementMap, err := vmtService.getDestinationFromVmturbo(pod)
	if err != nil {
		glog.Errorf("Error scheduling pod using vmturbo service: %s", err)
		dest, err := defaultScheduler.VMTScheduleHelper(pod)
		if err != nil {
			glog.Errorf("Error scheduling pod %s", pod.Namespace+"/"+pod.Name)
			return
		}
		placementMap = make(map[*api.Pod]string)
		placementMap[pod] = dest
	}

	for podToBeScheduled, destinationNodeName := range placementMap {
		vmtService.vmtScheduler.VMTSchedule(podToBeScheduled, destinationNodeName)
	}
}

// use vmt api to get reservation destinations
// TODO for now only deal with one pod at a time
// But the result is a map. Will change later when deploy works.
func (vmtService *VMTurboService) getDestinationFromVmturbo(pod *api.Pod) (map[*api.Pod]string, error) {
	deployRequest := deploy.NewDeployment(vmtService.config.Meta)

	// reservationResult is map[string]string -- [podName]nodeName
	// TODO !!!!!!! Now only support a single pod.
	return deployRequest.GetDestinationFromVmturbo(pod)
}

// Use a scheduler to bind or schedule
// TODO. This is not a good implementation. MUST Change
var defaultScheduler *scheduler.Scheduler

func SetSchedulerInstance(s *scheduler.Scheduler) error {
	if s == nil {
		return fmt.Errorf("Error! No scheduler instance")
	}
	defaultScheduler = s
	glog.V(3).Info("scheduler is set")
	return nil
}
