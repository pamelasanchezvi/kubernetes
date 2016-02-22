package api

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/golang/glog"
)

type VmtApi struct {
	vmtUrl    string
	extConfig map[string]string
}

const (
	logger = "VMTurbo API"
)

// Add a Kuberenets target to vmt ops manager
// example : http://localhost:8400/vmturbo/api/externaltargets?
//                     type=Kubernetes&nameOrAddress=10.10.150.2&username=AAA&targetIdentifier=A&password=Sysdreamworks123
func (vmtApi *VmtApi) AddK8sTarget(targetType, nameOrAddress, username, targetIdentifier, password string) error {
	fmt.Println("---------- Inside AddTarget() ----------")

	requestData := make(map[string]string)

	var requestDataBuffer bytes.Buffer

	requestData["type"] = targetType
	requestDataBuffer.WriteString("?type=")
	requestDataBuffer.WriteString(targetType)
	requestDataBuffer.WriteString("&")

	requestData["nameOrAddress"] = nameOrAddress
	requestDataBuffer.WriteString("nameOrAddress=")
	requestDataBuffer.WriteString(nameOrAddress)
	requestDataBuffer.WriteString("&")

	requestData["username"] = username
	requestDataBuffer.WriteString("username=")
	requestDataBuffer.WriteString(username)
	requestDataBuffer.WriteString("&")

	requestData["targetIdentifier"] = targetIdentifier
	requestDataBuffer.WriteString("targetIdentifier=")
	requestDataBuffer.WriteString(targetIdentifier)
	requestDataBuffer.WriteString("&")

	requestData["password"] = password
	requestDataBuffer.WriteString("password=")
	requestDataBuffer.WriteString(password)

	s := requestDataBuffer.String()
	// glog.V(3).Infof("parameters are %s", s)
	postResponse, err := vmtApi.apiPost("/externaltargets", s)
	if err != nil {
		return err
	}
	respMsg, err := vmtApi.parsePostResponse(postResponse)
	if err != nil {
		return err
	}
	glog.Infof("Add target response is %", respMsg)

	return nil
}

// Discover a target using api
// http://localhost:8400/vmturbo/api/targets/k8s_vmt
func (vmtApi *VmtApi) DiscoverTarget(nameOrAddress string) error {
	glog.V(4).Info("---------- Inside DiscoverTarget() ----------")

	postResponse, err := vmtApi.apiPost("/targets/"+nameOrAddress, "")
	if err != nil {
		return err
	}
	respMsg, err := vmtApi.parsePostResponse(postResponse)
	if err != nil {
		return err
	}
	glog.Infof("Discover target response is %", respMsg)

	return nil
}

// Create the reservation specification and
// return map which has pod name as key and node name as value
func (vmtApi *VmtApi) RequestPlacement(requestSpec, filterProperties map[string]string) (map[string]string, error) {
	glog.V(4).Info("---------- Inside RequestPlacement ----------")

	requestData := make(map[string]string)

	var requestDataBuffer bytes.Buffer

	if reservation_name, ok := requestSpec["reservation_name"]; !ok {
		glog.Errorf("---------- reservation name is not registered ----------")
		return nil, fmt.Errorf("reservation_name has not been registered.")
	} else {
		requestData["reservationName"] = reservation_name
		requestDataBuffer.WriteString("?reservationName=")
		requestDataBuffer.WriteString(reservation_name)
		requestDataBuffer.WriteString("&")
	}

	if num_instances, ok := requestSpec["num_instances"]; !ok {
		glog.Errorf("---------- num_instances not registered ----------")
		return nil, fmt.Errorf("num_instances has not been registered.")
	} else {
		requestData["count"] = num_instances
		requestDataBuffer.WriteString("count=")
		requestDataBuffer.WriteString(num_instances)
		requestDataBuffer.WriteString("&")
	}

	if template_name, ok := requestSpec["template_name"]; !ok {
		glog.Errorf("---------- template name is not registered ----------")
		return nil, fmt.Errorf("template_name has not been registered.")
	} else {
		requestData["templateName"] = template_name
		requestDataBuffer.WriteString("templateName=")
		requestDataBuffer.WriteString(template_name)
		requestDataBuffer.WriteString("&")
	}

	if deployment_profile, ok := requestSpec["deployment_profile"]; !ok {
		glog.Errorf("---------- deployment profile is not registered ----------")
		//return nil, fmt.Errorf("deployment_profile has not been registered.")
	} else {
		requestData["deploymentProfile"] = deployment_profile
		requestDataBuffer.WriteString("deploymentProfile=")
		requestDataBuffer.WriteString(deployment_profile)
		requestDataBuffer.WriteString("&")
	}

	// Append date and time
	requestDataBuffer.WriteString("deployDate=")
	// Must make sure space is escaped
	requestDataBuffer.WriteString("2015-10-11%2016:00:00")
	s := requestDataBuffer.String()
	glog.V(3).Infof("parameters are %s", s)
	postResponse, err := vmtApi.apiPost("/reservations", s)
	if err != nil {
		return nil, err
	}

	// get the reservation uuid
	reservationUUID, err := vmtApi.parsePostResponse(postResponse)
	if err != nil {
		return nil, err
	}
	glog.Infof("Reservation UUID is %s", string(reservationUUID))

	getResponse, err := vmtApi.apiGet("/reservations/" + reservationUUID)
	if err != nil {
		return nil, err
	}
	pod2nodeMap, err := vmtApi.parseGettReservationResponse(getResponse)
	if err != nil {
		return nil, err
	}
	return pod2nodeMap, nil
}

// call vmturbo api. return response
func (vmtApi *VmtApi) apiPost(postUrl, requestDataString string) (*http.Response, error) {
	fullUrl := "http://" + vmtApi.vmtUrl + "/vmturbo/api" + postUrl + requestDataString
	glog.V(4).Info("The full Url is ", fullUrl)
	req, err := http.NewRequest("POST", fullUrl, nil)

	req.SetBasicAuth(vmtApi.extConfig["Username"], vmtApi.extConfig["Password"])
	glog.V(4).Info(req)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		glog.Errorf("Error getting response: %s", err)
		return nil, err
	}

	respContent, _ := vmtApi.parsePostResponse(resp)
	glog.V(3).Infof("Post Succeed: %s", string(respContent))

	defer resp.Body.Close()
	return resp, nil
}

// call vmturbo api. return response
func (vmtApi *VmtApi) apiGet(getUrl string) (*http.Response, error) {
	fullUrl := "http://" + vmtApi.vmtUrl + "/vmturbo/api" + getUrl
	glog.V(4).Info("The full Url is ", fullUrl)
	req, err := http.NewRequest("POST", fullUrl, nil)

	req.SetBasicAuth(vmtApi.extConfig["Username"], vmtApi.extConfig["Password"])
	glog.V(4).Info(req)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		glog.Errorf("Error getting response: %s", err)
		return nil, err
	}

	glog.V(3).Info("Get Succeed")
	defer resp.Body.Close()
	return resp, nil
}

// this method takes in a reservation response and should return the reservation uuid, if there is any
func (vmtApi *VmtApi) parsePostResponse(resp *http.Response) (string, error) {
	if resp == nil {
		return "", fmt.Errorf("response sent in is nil")
	}

	content, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	glog.V(3).Infof("response content is %s", string(content))

	// TODO should parse the content. Currently don't know the correct post response content.
	return string(content), nil
}

// this method takes in a http get response for reservation and should return the reservation uuid, if there is any
func (vmtApi *VmtApi) parseGettReservationResponse(resp *http.Response) (map[string]string, error) {
	if resp == nil {
		return nil, fmt.Errorf("response sent in is nil")
	}

	content, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	glog.V(3).Infof("response content is %s", string(content))

	// TODO should parse the content. Currently don't know the correct get response content.
	pod2NodeMap := make(map[string]string)
	return pod2NodeMap, nil
}

func NewVmtApi(url string, externalConfiguration map[string]string) *VmtApi {
	return &VmtApi{
		vmtUrl:    url,
		extConfig: externalConfiguration,
	}
}
