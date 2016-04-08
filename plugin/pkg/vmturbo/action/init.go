package action

import (
	"k8s.io/kubernetes/plugin/pkg/vmturbo/probe/helper"

	"github.com/golang/glog"
)

var (
	localTestingFlag bool = false

	actionTestingFlag bool = false

	localTestStitchingIP string = ""
)

func init() {
	flag, err := helper.LoadTestingFlag("./plugin/pkg/vmturbo/probe/helper/testing_flag.json")
	if err != nil {
		glog.Errorf("Error initialize vmturbo package")
		return
	}
	localTestingFlag = flag.LocalTestingFlag
}
