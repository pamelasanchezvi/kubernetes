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
	"fmt"
	"runtime"

	"k8s.io/kubernetes/pkg/healthz"
	"k8s.io/kubernetes/pkg/util"
	"k8s.io/kubernetes/pkg/version/verflag"
	schedulerserver "k8s.io/kubernetes/plugin/cmd/kube-scheduler/app"
	"k8s.io/kubernetes/plugin/cmd/kube-vmturbo/app"

	"github.com/spf13/pflag"
)

func init() {
	healthz.DefaultHealthz()
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	fmt.Println("********** Run vmturbo service **********")

	s := app.NewVMTServer()
	s.AddFlags(pflag.CommandLine)

	util.InitFlags()
	util.InitLogs()
	defer util.FlushLogs()

	verflag.PrintAndExitIfRequested()

	go runScheduler(s)

	fmt.Println("********** Now after runScheduler **********")
	s.Run(pflag.CommandLine.Args())
}

// Start Kubernetes Scheduler from VMT service.
func runScheduler(vmtserver *app.VMTServer) {
	runtime.GOMAXPROCS(runtime.NumCPU())
	s := schedulerserver.NewVMTSchedulerServer(vmtserver)
	s.Master = vmtserver.Master
	s.Kubeconfig = vmtserver.Kubeconfig
	//s.AddFlags(fs)
	s.BindPodsQPS = 15.0
	s.BindPodsBurst = 20
	s.EnableProfiling = true

	s.RunVMTScheduler(pflag.CommandLine.Args())
}
