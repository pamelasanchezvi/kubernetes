package deploy

import (
	// "fmt"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client"
	"k8s.io/kubernetes/pkg/client/cache"
	"k8s.io/kubernetes/pkg/client/record"
	"k8s.io/kubernetes/pkg/util"
	"k8s.io/kubernetes/plugin/pkg/scheduler"
	// "k8s.io/kubernetes/plugin/pkg/scheduler/algorithm"
	"k8s.io/kubernetes/plugin/pkg/scheduler/metrics"

	"github.com/golang/glog"
)

type Config struct {
	// It is expected that changes made via modeler will be observed
	// by MinionLister and Algorithm.
	Modeler scheduler.SystemModeler
	Binder  scheduler.Binder

	// Rate at which we can create pods
	BindPodsRateLimiter util.RateLimiter

	// Recorder is the EventRecorder to use
	Recorder record.EventRecorder
}

type VMTScheduler struct {
	config *Config
}

func NewVMTScheduler(kubeClient *client.Client) *VMTScheduler {
	scheduledPodLister := &cache.StoreToPodLister{}
	podQueue := cache.NewFIFO(cache.MetaNamespaceKeyFunc)

	modeler := scheduler.NewSimpleModeler(&cache.StoreToPodLister{Store: podQueue}, scheduledPodLister)

	bindPodsQPS := float32(15.0)
	bindPodsBurst := 20
	rateLimiter := util.NewTokenBucketRateLimiter(bindPodsQPS, bindPodsBurst)

	config := &Config{
		Modeler:             modeler,
		Binder:              &binder{kubeClient},
		BindPodsRateLimiter: rateLimiter,
	}
	eventBroadcaster := record.NewBroadcaster()
	config.Recorder = eventBroadcaster.NewRecorder(api.EventSource{Component: "scheduler"})
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(kubeClient.Events(""))

	return &VMTScheduler{
		config: config,
	}
}

func (s *VMTScheduler) VMTSchedule(pod *api.Pod, dest string) {
	if s.config.BindPodsRateLimiter != nil {
		s.config.BindPodsRateLimiter.Accept()
	}

	start := time.Now()
	defer func() {
		metrics.E2eSchedulingLatency.Observe(metrics.SinceInMicroseconds(start))
	}()
	metrics.SchedulingAlgorithmLatency.Observe(metrics.SinceInMicroseconds(start))

	b := &api.Binding{
		ObjectMeta: api.ObjectMeta{Namespace: pod.Namespace, Name: pod.Name},
		Target: api.ObjectReference{
			Kind: "Node",
			Name: dest,
		},
	}

	// We want to add the pod to the model iff the bind succeeds, but we don't want to race
	// with any deletions, which happen asynchronously.
	s.config.Modeler.LockedAction(func() {
		bindingStart := time.Now()
		err := s.config.Binder.Bind(b)
		metrics.BindingLatency.Observe(metrics.SinceInMicroseconds(bindingStart))
		if err != nil {
			glog.V(1).Infof("Failed to bind pod: %+v", err)
			s.config.Recorder.Eventf(pod, "FailedScheduling", "Binding rejected: %v", err)
			// s.config.Error(pod, err)
			return
		}
		s.config.Recorder.Eventf(pod, "Scheduled", "Successfully assigned %v to %v", pod.Name, dest)
		// tell the model to assume that this binding took effect.
		assumed := *pod
		assumed.Spec.NodeName = dest
		s.config.Modeler.AssumePod(&assumed)

	})
}

type binder struct {
	*client.Client
}

// Bind just does a POST binding RPC.
func (b *binder) Bind(binding *api.Binding) error {
	glog.V(2).Infof("Attempting to bind %v to %v", binding.Name, binding.Target.Name)
	ctx := api.WithNamespace(api.NewContext(), binding.Namespace)
	return b.Post().Namespace(api.NamespaceValue(ctx)).Resource("bindings").Body(binding).Do().Error()
	// TODO: use Pods interface for binding once clusters are upgraded
	// return b.Pods(binding.Namespace).Bind(binding)
}
