package defaultscheduler

import (
	"k8s.io/kubernetes/pkg/client"
	"k8s.io/kubernetes/pkg/util"
	"k8s.io/kubernetes/plugin/pkg/scheduler"
	"k8s.io/kubernetes/plugin/pkg/scheduler/factory"

	"github.com/golang/glog"
)

// The same method defined in scheduler server. Create config file for the default kubernetes scheduler.
func createConfigFromDefaultProvider(configFactory *factory.ConfigFactory) (*scheduler.Config, error) {

	// if the config file isn't provided, use the specified (or default) provider
	// check of algorithm provider is registered and fail fast
	_, err := factory.GetAlgorithmProvider(factory.DefaultProvider)
	if err != nil {
		return nil, err
	}

	return configFactory.CreateFromProvider(factory.DefaultProvider)
}

func CreateConfig(kubeClient *client.Client) *scheduler.Config {
	configFactory := factory.NewConfigFactory(kubeClient, util.NewTokenBucketRateLimiter(15.0, 20.0))
	config, err := createConfigFromDefaultProvider(configFactory)
	if err != nil {
		glog.Fatalf("Failed to create scheduler configuration: %v", err)
	}

	return config
}
