package vmturbo

import (
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client"
	"k8s.io/kubernetes/pkg/client/cache"
	"k8s.io/kubernetes/pkg/fields"
	// "k8s.io/kubernetes/pkg/storage"

	"k8s.io/kubernetes/plugin/pkg/vmturbo/storage"
	vmtcache "k8s.io/kubernetes/plugin/pkg/vmturbo/vmt/cache"
	vmtmeta "k8s.io/kubernetes/plugin/pkg/vmturbo/vmt/metadata"
	"k8s.io/kubernetes/plugin/pkg/vmturbo/vmt/registry"

	vmtstorage "k8s.io/kubernetes/plugin/pkg/vmturbo/storage"
)

// Meta stores VMT Metadata.
type Config struct {
	Client        *client.Client
	Meta          *vmtmeta.VMTMeta
	EtcdStorage   storage.Storage
	NodeQueue     *vmtcache.HashedFIFO
	PodQueue      *vmtcache.HashedFIFO
	VMTEventQueue *vmtcache.HashedFIFO
	// Close this to stop all reflectors
	StopEverything chan struct{}
}

// Create a vmturbo config
func NewVMTConfig(client *client.Client, etcdStorage storage.Storage, meta *vmtmeta.VMTMeta) *Config {
	config := &Config{
		Client:         client,
		Meta:           meta,
		EtcdStorage:    etcdStorage,
		NodeQueue:      vmtcache.NewHashedFIFO(cache.MetaNamespaceKeyFunc),
		PodQueue:       vmtcache.NewHashedFIFO(cache.MetaNamespaceKeyFunc),
		VMTEventQueue:  vmtcache.NewHashedFIFO(cache.MetaNamespaceKeyFunc),
		StopEverything: make(chan struct{}),
	}

	// Watch minions.
	// Minions may be listed frequently, so provide a local up-to-date cache.
	cache.NewReflector(config.createMinionLW(), &api.Node{}, config.NodeQueue, 0).RunUntil(config.StopEverything)

	// monitor unassigned pod
	cache.NewReflector(config.createUnassignedPodLW(), &api.Pod{}, config.PodQueue, 0).RunUntil(config.StopEverything)

	// monitor vmtevents
	vmtstorage.NewReflector(config.createVMTEventLW(), &registry.VMTEvent{}, config.VMTEventQueue, 0).RunUntil(config.StopEverything)

	return config
}

// Create a list and watch for node to filter out nodes those cannot be scheduled.
func (c *Config) createMinionLW() *cache.ListWatch {
	fields := fields.Set{client.NodeUnschedulable: "false"}.AsSelector()
	return cache.NewListWatchFromClient(c.Client, "nodes", api.NamespaceAll, fields)
}

// Returns a cache.ListWatch that finds all pods that are
// already scheduled.
// This method is not used
func (c *Config) createAssignedPodLW() *cache.ListWatch {
	return cache.NewListWatchFromClient(c.Client, "pods", api.NamespaceAll,
		parseSelectorOrDie(client.PodHost+"!="))
}

// Returns a cache.ListWatch that finds all pods that need to be
// scheduled.
func (c *Config) createUnassignedPodLW() *cache.ListWatch {
	return cache.NewListWatchFromClient(c.Client, "pods", api.NamespaceAll, fields.Set{client.PodHost: ""}.AsSelector())
}

// VMTEvent ListWatch
func (c *Config) createVMTEventLW() *vmtstorage.ListWatch {
	return vmtstorage.NewListWatchFromStorage(c.EtcdStorage, "vmtevents", api.NamespaceAll, nil)
}

func parseSelectorOrDie(s string) fields.Selector {
	selector, err := fields.ParseSelector(s)
	if err != nil {
		panic(err)
	}
	return selector
}
