package main

import (
	"runtime"
	"strconv"

	"k8s.io/kubernetes/pkg/util"
	"k8s.io/kubernetes/pkg/version/verflag"

	"k8s.io/kubernetes/plugin/cmd/kube-vmtactionsimulator/builder"
	vmtaction "k8s.io/kubernetes/plugin/pkg/vmturbo/action"

	"github.com/golang/glog"
	"github.com/spf13/pflag"
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	builder := builder.NewSimulatorBuilder()
	builder.AddFlags(pflag.CommandLine)

	util.InitFlags()
	util.InitLogs()
	defer util.FlushLogs()

	verflag.PrintAndExitIfRequested()

	builder.Init(pflag.CommandLine.Args())
	simulator, err := builder.Build()
	if err != nil {
		glog.Errorf("error getting simulator: %s", err)
		return
	}

	action := simulator.Action()
	namespace := simulator.Namespace()

	// The simulator can simulate move, get and provision action now.
	actor := vmtaction.NewKubeActor(simulator.KubeClient())
	if action == "move" || action == "Move " {
		podToMove := simulator.PodToMove()
		destinationNode := simulator.Destination()
		actor.MovePod(podToMove, namespace, destinationNode, -1)
		return
	} else if action == "get" {
		actor.GetAllNodes()
		return
	} else if action == "provision" {
		podLabel := simulator.Label()
		newReplicas, _ := strconv.Atoi(simulator.NewReplica())
		actor.UpdateReplicas(podLabel, namespace, newReplicas)
		return
	}
}
