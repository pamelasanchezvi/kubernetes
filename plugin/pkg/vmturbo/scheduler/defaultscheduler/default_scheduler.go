package defaultscheduler

import (
	"fmt"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client"
	"k8s.io/kubernetes/plugin/pkg/scheduler/metrics"

	"k8s.io/kubernetes/plugin/pkg/scheduler"

	"github.com/golang/glog"
)

// Start default Kubernetes scheduler from VMT service.
// Although Kubeturbo provides pod scheduling from VMturbo's reservation API, the default
// Kubernetes scheduler serves as a backup when there is any issue getting deploy destination
// from VMTurbo server.
type DefaultScheduler struct {
	config *scheduler.Config
}

func NewDefaultScheduler(kubeClient *client.Client) *DefaultScheduler {
	c := CreateConfig(kubeClient)
	s := &DefaultScheduler{
		config: c,
	}
	metrics.Register()
	return s
}

// TODO. This should be removed when the vmt reservation api works.
// Call the built in schduling algorithm to schedule a pod
func (s *DefaultScheduler) FindDestination(pod *api.Pod) (string, error) {
	dest, err := s.config.Algorithm.Schedule(pod, s.config.MinionLister)
	if err != nil {
		glog.Errorf("Error Scheduling pod %+v", pod)
		return "", fmt.Errorf("Error Scheduling pod %+v", pod)
	}
	return dest, nil
}
