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

package main

import (
	"runtime"

	"k8s.io/kubernetes/pkg/healthz"
	"k8s.io/kubernetes/pkg/util"
	"k8s.io/kubernetes/pkg/version/verflag"
	"k8s.io/kubernetes/plugin/cmd/kube-vmturbo/app"

	"github.com/golang/glog"
	"github.com/spf13/pflag"
)

func init() {
	healthz.DefaultHealthz()
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	glog.V(2).Infof("*** Run Kubeturbo service ***")

	s := app.NewVMTServer()
	s.AddFlags(pflag.CommandLine)

	util.InitFlags()
	util.InitLogs()
	defer util.FlushLogs()

	verflag.PrintAndExitIfRequested()

	go runScheduler(s)

	s.Run(pflag.CommandLine.Args())
}

// Start default Kubernetes scheduler from VMT service.
// Although Kubeturbo provides pod scheduling from VMturbo's reservation API, the default
// Kubernetes scheduler serves as a backup when there is any issue getting deploy destination
// from VMTurbo server.
func runScheduler(vmtserver *app.VMTServer) {
	glog.V(3).Infof("Creating default Kubernetes scheduler")
	runtime.GOMAXPROCS(runtime.NumCPU())
	defaultSchedulerServer := app.NewDefaultK8sSchedulerServer(vmtserver)
	defaultSchedulerServer.Master = vmtserver.Master
	defaultSchedulerServer.Kubeconfig = vmtserver.Kubeconfig
	defaultSchedulerServer.BindPodsQPS = 15.0
	defaultSchedulerServer.BindPodsBurst = 20
	defaultSchedulerServer.EnableProfiling = true

	defaultSchedulerServer.RunDefaultK8sScheduler(pflag.CommandLine.Args())
}
