/*
Copyright 2014 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package app implements a Server object for running the scheduler.
package app

import (
	"fmt"
	"io/ioutil"
	"math"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/latest"
	"k8s.io/kubernetes/pkg/api/meta"
	"k8s.io/kubernetes/pkg/client"
	"k8s.io/kubernetes/pkg/client/clientcmd"
	clientcmdapi "k8s.io/kubernetes/pkg/client/clientcmd/api"
	"k8s.io/kubernetes/pkg/client/record"
	"k8s.io/kubernetes/pkg/healthz"
	"k8s.io/kubernetes/pkg/master"
	"k8s.io/kubernetes/pkg/master/ports"
	"k8s.io/kubernetes/pkg/tools"
	"k8s.io/kubernetes/pkg/util"
	schedulerbuilder "k8s.io/kubernetes/plugin/cmd/kube-scheduler/app"
	"k8s.io/kubernetes/plugin/pkg/scheduler"
	_ "k8s.io/kubernetes/plugin/pkg/scheduler/algorithmprovider"
	schedulerapi "k8s.io/kubernetes/plugin/pkg/scheduler/api"
	latestschedulerapi "k8s.io/kubernetes/plugin/pkg/scheduler/api/latest"
	"k8s.io/kubernetes/plugin/pkg/scheduler/factory"

	"k8s.io/kubernetes/plugin/pkg/vmturbo"
	"k8s.io/kubernetes/plugin/pkg/vmturbo/conversion"
	"k8s.io/kubernetes/plugin/pkg/vmturbo/metadata"
	"k8s.io/kubernetes/plugin/pkg/vmturbo/registry"
	"k8s.io/kubernetes/plugin/pkg/vmturbo/storage"
	etcdhelper "k8s.io/kubernetes/plugin/pkg/vmturbo/storage/etcd"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/pflag"
)

var (
	defaultSchedulerInstance *scheduler.Scheduler
)

// VMTServer has all the context and params needed to run a Scheduler
type VMTServer struct {
	Port                  int
	Address               net.IP
	Master                string
	MetaConfigPath        string
	Kubeconfig            string
	BindPodsQPS           float32
	BindPodsBurst         int
	EtcdServerList        []string
	EtcdCA                string
	EtcdClientCertificate string
	EtcdClientKey         string
	EtcdConfigFile        string
	EtcdPathPrefix        string
}

// NewVMTServer creates a new VMTServer with default parameters
func NewVMTServer() *VMTServer {
	s := VMTServer{
		Port:    ports.VMTPort,
		Address: net.ParseIP("127.0.0.1"),
	}
	return &s
}

// AddFlags adds flags for a specific VMTServer to the specified FlagSet
func (s *VMTServer) AddFlags(fs *pflag.FlagSet) {
	fs.IntVar(&s.Port, "port", s.Port, "The port that the scheduler's http service runs on")
	fs.StringVar(&s.Master, "master", s.Master, "The address of the Kubernetes API server (overrides any value in kubeconfig)")
	fs.StringVar(&s.MetaConfigPath, "config-path", s.MetaConfigPath, "The path to the vmt config file.")
	fs.StringVar(&s.Kubeconfig, "kubeconfig", s.Kubeconfig, "Path to kubeconfig file with authorization and master location information.")
	fs.StringSliceVar(&s.EtcdServerList, "etcd-servers", s.EtcdServerList, "List of etcd servers to watch (http://ip:port), comma separated. Mutually exclusive with -etcd-config")
	fs.StringVar(&s.EtcdCA, "cacert", s.EtcdCA, "Path to etcd ca.")
	fs.StringVar(&s.EtcdClientCertificate, "client-cert", s.EtcdClientCertificate, "Path to etcd client certificate")
	fs.StringVar(&s.EtcdClientKey, "client-key", s.EtcdClientKey, "Path to etcd client key")
}

// Run runs the specified VMTServer.  This should never exit.
func (s *VMTServer) Run(_ []string) error {
	if s.Kubeconfig == "" && s.Master == "" {
		glog.Warningf("Neither --kubeconfig nor --master was specified.  Using default API client.  This might not work.")
	}

	glog.V(3).Infof("Master is %s", s.Master)

	if s.MetaConfigPath == "" {
		glog.Fatalf("The path to the VMT config file is not provided.Exiting...")
		os.Exit(1)
	}

	if (s.EtcdConfigFile != "" && len(s.EtcdServerList) != 0) || (s.EtcdConfigFile == "" && len(s.EtcdServerList) == 0) {
		glog.Fatalf("specify either --etcd-servers or --etcd-config")
	}

	// This creates a client, first loading any specified kubeconfig
	// file, and then overriding the Master flag, if non-empty.
	kubeconfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: s.Kubeconfig},
		&clientcmd.ConfigOverrides{ClusterInfo: clientcmdapi.Cluster{Server: s.Master}}).ClientConfig()
	if err != nil {
		glog.Errorf("Error getting kubeconfig:  %s", err)
		return err
	}
	// This specifies the number and the max number of query per second to the api server.
	kubeconfig.QPS = 20.0
	kubeconfig.Burst = 30

	kubeClient, err := client.New(kubeconfig)
	if err != nil {
		glog.Fatalf("Invalid API configuration: %v", err)
	}

	// TODO not clear
	// go func() {
	// 	mux := http.NewServeMux()
	// 	healthz.InstallHandler(mux)
	// 	if s.EnableProfiling {
	// 		mux.HandleFunc("/debug/pprof/", pprof.Index)
	// 		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	// 		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	// 	}
	// 	mux.Handle("/metrics", prometheus.Handler())

	// 	server := &http.Server{
	// 		Addr:    net.JoinHostPort(s.Address.String(), strconv.Itoa(s.Port)),
	// 		Handler: mux,
	// 	}
	// 	glog.Fatal(server.ListenAndServe())
	// }()

	// serverAddr, targetType, nameOrAddress, targetIdentifier, password
	vmtMeta, err := metadata.NewVMTMeta(s.MetaConfigPath)
	if err != nil {
		glog.Errorf("Get error when loading configurations: %s", err)
		os.Exit(1)
	}
	glog.V(3).Infof("Finished loading configuration from %s", s.MetaConfigPath)

	etcdclientBuilder := etcdhelper.NewEtcdClientBuilder().ServerList(s.EtcdServerList).SetTransport(s.EtcdCA, s.EtcdClientCertificate, s.EtcdClientKey)
	etcdClient, err := etcdclientBuilder.CreateAndTest()
	if err != nil {
		glog.Errorf("Error creating etcd client instance for vmt service: %s", err)
		return err
	}

	s.EtcdPathPrefix = master.DefaultEtcdPathPrefix
	etcdStorage, err := newEtcd(etcdClient, latest.InterfacesFor, latest.Version, "", s.EtcdPathPrefix)
	if err != nil {
		glog.Warningf("Error creating etcd storage instance for vmt service: %s", err)
		return err
	}

	vmtConfig := vmturbo.NewVMTConfig(kubeClient, etcdStorage, vmtMeta)

	vmtService := vmturbo.NewVMTurboService(vmtConfig)

	// Make sure default scheduler is set. If certain time is passed, then there is a fatal error.
	timeDelayFactor := 0
	for defaultSchedulerInstance == nil && timeDelayFactor < 10 {
		glog.V(3).Infof("Default scheduler is not set yet")
		time.Sleep(time.Millisecond * 100 * time.Duration(math.Pow(2, float64(timeDelayFactor))))
		timeDelayFactor += 1
	}
	if defaultSchedulerInstance == nil {
		glog.Fatalf("Failed to set Default Scheduler. Exiting..")
	}

	vmtService.TurboScheduler.SetDefaultScheduler(defaultSchedulerInstance)

	vmtService.Run()

	select {}
}

func newEtcd(client tools.EtcdClient, interfacesFunc meta.VersionInterfacesFunc, defaultVersion, storageVersion, pathPrefix string) (etcdStorage storage.Storage, err error) {
	if storageVersion == "" {
		storageVersion = defaultVersion
	}

	master.NewEtcdStorage(client, interfacesFunc, storageVersion, pathPrefix)

	simpleCodec := conversion.NewSimpleCodec()
	simpleCodec.AddKnownTypes(&registry.VMTEvent{})
	simpleCodec.AddKnownTypes(&registry.VMTEventList{})
	return etcdhelper.NewEtcdStorage(client, simpleCodec, pathPrefix), nil
}

type DefaultK8sSchedulerServer struct {
	*schedulerbuilder.SchedulerServer
}

func NewDefaultK8sSchedulerServer(vmtServer *VMTServer) *DefaultK8sSchedulerServer {
	s := schedulerbuilder.SchedulerServer{
		Port:              ports.VMTPort,
		Address:           net.ParseIP("127.0.0.1"),
		AlgorithmProvider: factory.DefaultProvider,
	}
	return &DefaultK8sSchedulerServer{&s}
}

// The same method defined in scheduler server. Create config file for the default kubernetes scheduler.
func (s DefaultK8sSchedulerServer) createConfig(configFactory *factory.ConfigFactory) (*scheduler.Config, error) {
	var policy schedulerapi.Policy
	var configData []byte

	if _, err := os.Stat(s.PolicyConfigFile); err == nil {
		configData, err = ioutil.ReadFile(s.PolicyConfigFile)
		if err != nil {
			return nil, fmt.Errorf("Unable to read policy config: %v", err)
		}
		err = latestschedulerapi.Codec.DecodeInto(configData, &policy)
		if err != nil {
			return nil, fmt.Errorf("Invalid configuration: %v", err)
		}

		return configFactory.CreateFromConfig(policy)
	}

	// if the config file isn't provided, use the specified (or default) provider
	// check of algorithm provider is registered and fail fast
	_, err := factory.GetAlgorithmProvider(s.AlgorithmProvider)
	if err != nil {
		return nil, err
	}

	return configFactory.CreateFromProvider(s.AlgorithmProvider)
}

// Run runs the specified SchedulerServer.  This should never exit. Same code from schduler server.
func (s *DefaultK8sSchedulerServer) RunDefaultK8sScheduler(_ []string) error {
	if s.Kubeconfig == "" && s.Master == "" {
		glog.Warningf("Neither --kubeconfig nor --master was specified.  Using default API client.  This might not work.")
	}

	// This creates a client, first loading any specified kubeconfig
	// file, and then overriding the Master flag, if non-empty.
	kubeconfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: s.Kubeconfig},
		&clientcmd.ConfigOverrides{ClusterInfo: clientcmdapi.Cluster{Server: s.Master}}).ClientConfig()
	if err != nil {
		return err
	}
	kubeconfig.QPS = 20.0
	kubeconfig.Burst = 30

	kubeClient, err := client.New(kubeconfig)
	if err != nil {
		glog.Fatalf("Invalid API configuration: %v", err)
	}

	go func() {
		mux := http.NewServeMux()
		healthz.InstallHandler(mux)
		if s.EnableProfiling {
			mux.HandleFunc("/debug/pprof/", pprof.Index)
			mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
			mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		}
		mux.Handle("/metrics", prometheus.Handler())

		server := &http.Server{
			Addr:    net.JoinHostPort(s.Address.String(), strconv.Itoa(s.Port)),
			Handler: mux,
		}
		glog.Fatal(server.ListenAndServe())
	}()

	configFactory := factory.NewConfigFactory(kubeClient, util.NewTokenBucketRateLimiter(s.BindPodsQPS, s.BindPodsBurst))
	config, err := s.createConfig(configFactory)
	if err != nil {
		glog.Fatalf("Failed to create scheduler configuration: %v", err)
	}

	eventBroadcaster := record.NewBroadcaster()
	config.Recorder = eventBroadcaster.NewRecorder(api.EventSource{Component: "scheduler"})
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(kubeClient.Events(""))

	sched := scheduler.New(config)
	// vmturbo.SetSchedulerInstance(sched)

	defaultSchedulerInstance = sched

	select {}
}
