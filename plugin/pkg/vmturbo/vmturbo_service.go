package vmturbo

import (
	"fmt"
	"strings"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/util"
	"k8s.io/kubernetes/plugin/pkg/scheduler"

	"k8s.io/kubernetes/plugin/pkg/vmturbo/action"
	vmtapi "k8s.io/kubernetes/plugin/pkg/vmturbo/api"
	"k8s.io/kubernetes/plugin/pkg/vmturbo/registry"
	comm "k8s.io/kubernetes/plugin/pkg/vmturbo/vmturbocommunicator"

	"github.com/vmturbo/vmturbo-go-sdk/sdk"

	"github.com/golang/glog"
)

type VMTurboService struct {
	config       *Config
	vmtcomm      *comm.VMTCommunicator
	vmtEventChan chan *registry.VMTEvent
}

func NewVMTurboService(c *Config) *VMTurboService {
	vmtService := &VMTurboService{
		config: c,
	}
	vmtService.vmtEventChan = make(chan *registry.VMTEvent)
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

// When new node added in, this function is called. Otherwise, it is blocked.
func (v *VMTurboService) getNextVMTEvent() {
	event := v.config.VMTEventQueue.Pop().(*registry.VMTEvent)
	glog.Infof("Get a new Event from etcd: %v", event)
	if event.ActionType == "move" || event.ActionType == "provision" {
		glog.V(2).Infof("Get a valid vmtevent from etcd.")

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

	// for test vmtevents etcd registry
	vmtEvents := registry.NewVMTEvents(v.config.Client, "", v.config.EtcdStorage)
	event := registry.GenerateVMTEvent("create", pod.Namespace, pod.Name, "1.0.0.0", 1)
	_, errorPost := vmtEvents.Create(event)
	if errorPost != nil {
		glog.Errorf("Error posting vmtevent: %s\n", errorPost)
	}

	go vmtEvents.Watch(0)

	// getEvent, err := vmtEvents.Get()
	// if err != nil {
	// 	glog.Errorf("Error is %s", err)
	// } // else {
	// // 	// glog.Infof("Get %+v", getEvent)
	// // }

	// ----------------- try channel and vmtevent -------------
	select {
	case vmtEventFromEtcd := <-v.vmtEventChan:
		if vmtEventFromEtcd.ActionType == "move" {
			glog.V(3).Infof("Schedule destination from etcd is %s.", vmtEventFromEtcd.Destination)
			glog.V(2).Infof("Move Pod %v to %s", pod.Name, vmtEventFromEtcd.Destination)

			vmtScheduler.VMTSchedule(pod, vmtEventFromEtcd.Destination)
		} else if vmtEventFromEtcd.ActionType == "provision" {
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
			glog.V(3).Infof("Get vmtevent %v", vmtEventFromEtcd)
			glog.V(3).Infof("Send action response")
			progress := int32(100)
			v.vmtcomm.SendActionReponse(sdk.ActionResponseState_SUCCEEDED, progress, int32(vmtEventFromEtcd.VMTMessageID), "Success")
		}
		v.vmtcomm.DiscoverTarget()
		return

	default:
		glog.V(3).Infof("Nothing from etcd. Try to use simulator.")
		// This if block is for test move action only.
		// if the pod is generated from move action, then after scheduler, it should avoid the following normal
		// schedule and return
		moveSimulator := &action.MoveSimulator{}
		if destination, msgID := moveSimulator.IsMovePod(); destination != "" {
			glog.V(2).Infof("Move Pod %v to %s", pod.Name, destination)

			vmtScheduler.VMTSchedule(pod, destination)
			// TODO, at this point, we really do not know if the assignment of the pod succeeds or not.
			// The right place of sending move reponse is after the event.
			// Here for test purpose, send the move success action response.
			if msgID > -1 {
				v.vmtcomm.SendActionReponse(sdk.ActionResponseState_SUCCEEDED, 100, msgID, "Success")
			}
			v.vmtcomm.DiscoverTarget()
			return
		}
	}
	v.schedule(pod)
}

func (vmtService *VMTurboService) schedule(pod *api.Pod) {
	placementMap, err := vmtService.getDestinationFromVmturbo(pod)
	if err != nil {
		glog.Errorf("error: ", err)
	}

	if placementMap == nil {
		// In this case, still wait pods controlled by the same replication controller
		// replicas number should be the same with the number of pods
		return
	}

	for podToBeScheduled, destinationNodeName := range placementMap {
		vmtScheduler.VMTSchedule(podToBeScheduled, destinationNodeName)
	}
}

// use vmt api to get reservation destinations
// TODO for now only deal with one pod at a time
// But the result is a map. Will change later when deploy works.
func (vmtService *VMTurboService) getDestinationFromVmturbo(pod *api.Pod) (map[*api.Pod]string, error) {

	extCongfix := make(map[string]string)
	extCongfix["Username"] = vmtService.config.Meta.OpsManagerUsername
	extCongfix["Password"] = vmtService.config.Meta.OpsManagerPassword
	vmtapi.NewVmtApi(vmtService.config.Meta.ServerAddress, extCongfix)
	// vmturboApi := vmtapi.NewVmtApi(vmtUrl, extCongfix)

	// requestSpec := getRequestSpec(pod)

	// // reservationResult is map[string]string -- [podName]nodeName
	// // TODO !!!!!!! This should be called and return the map
	// vmturboApi.RequestPlacement(requestSpec, nil)

	//-----------------The following is for the test purpose-----------------
	// After deploy framework works, it will get destination from vmt reservation api.
	reservationResult := make(map[string]string)
	dest, err := vmtScheduler.VMTScheduleHelper(pod)
	if err != nil {
		return nil, nil
	}
	reservationResult[pod.Name] = dest

	//-----------------------------------------------------------------------

	placementMap := make(map[*api.Pod]string)
	// currently only deal with one pod
	if nodeName, ok := reservationResult[pod.Name]; ok {
		placementMap[pod] = nodeName
	}
	return placementMap, nil
}

// Get the request specification, basically the additional data that should be sent with post
func getRequestSpec(pod *api.Pod) map[string]string {
	requestSpec := make(map[string]string)
	requestSpec["reservation_name"] = "kubernetesReservationTest"
	requestSpec["num_instances"] = "1"
	// TODO, here we must get the UUID or Name of PodProfile.
	requestSpec["template_name"] = "DC6_1CxZMJkEEeCdJOYu6"

	return requestSpec
}

// Use a scheduler to bind or schedule
// TODO. This is not a good implementation. MUST Change
var vmtScheduler *scheduler.Scheduler

func SetSchedulerInstance(s *scheduler.Scheduler) error {
	if s == nil {
		return fmt.Errorf("Error! No scheduler instance")
	}
	vmtScheduler = s
	glog.V(3).Info("scheduler is set")
	return nil
}
