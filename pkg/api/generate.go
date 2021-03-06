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

package api

import (
	"fmt"
	"io/ioutil"
	"strings"
	"time"

	"github.com/golang/glog"

	utilrand "k8s.io/kubernetes/pkg/util/rand"
)

// NameGenerator generates names for objects. Some backends may have more information
// available to guide selection of new names and this interface hides those details.
type NameGenerator interface {
	// GenerateName generates a valid name from the base name, adding a random suffix to the
	// the base. If base is valid, the returned name must also be valid. The generator is
	// responsible for knowing the maximum valid name length.
	GenerateName(base string) string
}

// GenerateName will resolve the object name of the provided ObjectMeta to a generated version if
// necessary. It expects that validation for ObjectMeta has already completed (that Base is a
// valid name) and that the NameGenerator generates a name that is also valid.
func GenerateName(u NameGenerator, meta *ObjectMeta) {
	if len(meta.GenerateName) == 0 || len(meta.Name) != 0 {
		return
	}
	meta.Name = u.GenerateName(meta.GenerateName)
}

// simpleNameGenerator generates random names.
type simpleNameGenerator struct{}

// SimpleNameGenerator is a generator that returns the name plus a random suffix of five alphanumerics
// when a name is requested. The string is guaranteed to not exceed the length of a standard Kubernetes
// name (63 characters)
var SimpleNameGenerator NameGenerator = simpleNameGenerator{}

const (
	// TODO: make this flexible for non-core resources with alternate naming rules.
	maxNameLength          = 63
	randomLength           = 5
	maxGeneratedNameLength = maxNameLength - randomLength
)

func (simpleNameGenerator) GenerateName(base string) string {
	if len(base) > maxGeneratedNameLength {
		base = base[:maxGeneratedNameLength]
	}
	gname := getNameOfMovedPod(base)
	fmt.Printf("$$$$$$$$$$$$$$$$ gname is %s $$$$$$$$$$$$$$$$$$$$$\n", gname)
	if gname == "" {
		gname = utilrand.String(randomLength)
	}
	fmt.Printf("$$$$$$$$$$$$$$$$ gname is %s $$$$$$$$$$$$$$$$$$$$$\n", gname)

	return fmt.Sprintf("%s%s", base, gname)
}

func read(filePath string) (string, error) {
	dat, err := ioutil.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	fmt.Print(string(dat))
	return string(dat), nil
}

// Write action description to the specified file.
func write(actionDescription, filePath string) error {
	// To start, here’s how to dump a string (or just bytes) into a file.
	ad := []byte(actionDescription)
	err := ioutil.WriteFile(filePath, ad, 0644)
	if err != nil {
		return err
	}
	return nil
}

// the info is stored in this way.
// Action\tPod\tDestination\tMsgId.
// TODO. This might be stored and implemented using etcd.
func getNameOfMovedPod(base string) (name string) {
	time.Sleep(1 * time.Second)
	filePath := "/tmp/dat1"
	entry, err := read(filePath)
	if err != nil {
		glog.Errorf("Error reading in %s: %s", filePath, err)
		return ""
	}

	err = write("", filePath)
	if err != nil {
		glog.Errorf("Error clearing %s: %s", filePath, err)
		return ""
	}

	content := strings.Split(string(entry), "\t")
	if len(content) < 4 {
		return ""
	}
	podName := content[1]

	podNameContent := strings.Split(podName, base)
	if len(podNameContent) < 2 {
		return ""
	}

	return podNameContent[1]

}
