package metadata

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
)

const (
	// Appliance address.
	// SERVER_ADDRESS string = "10.10.173.193:80"

	// SERVER_ADDRESS string = "10.10.192.78:8080"

	SERVER_ADDRESS string = "10.10.200.114:8080"

	// SERVER_ADDRESS string = "192.168.1.4:8080"

	// SERVER_ADDRESS string = "192.168.1.4:8080"

	TARGET_TYPE string = "Kubernetes"

	NAME_OR_ADDRESS string = "k8s_vmt"

	USERNAME string = "kubernetes_user"

	TARGET_IDENTIFIER string = "my_k8s"

	PASSWORD string = "fake_password"

	// Ops Manager related
	OPS_MGR_USRN = "administrator"
	OPS_MGR_PSWD = "a"

	//WebSocket related
	LOCAL_ADDRESS    = "http://172.16.201.167/"
	WS_SERVER_USRN   = "vmtRemoteMediation"
	WS_SERVER_PASSWD = "vmtRemoteMediation"
)

type VMTMeta struct {
	ServerAddress      string
	TargetType         string
	NameOrAddress      string
	Username           string
	TargetIdentifier   string
	Password           string
	LocalAddress       string
	WebSocketUsername  string
	WebSocketPassword  string
	OpsManagerUsername string
	OpsManagerPassword string
}

func NewVMTMeta(metaConfigFilePath string) *VMTMeta {
	meta := &VMTMeta{
		ServerAddress:      SERVER_ADDRESS,
		TargetType:         TARGET_TYPE,
		NameOrAddress:      NAME_OR_ADDRESS,
		Username:           USERNAME,
		TargetIdentifier:   TARGET_IDENTIFIER,
		Password:           PASSWORD,
		LocalAddress:       LOCAL_ADDRESS,
		WebSocketUsername:  WS_SERVER_USRN,
		WebSocketPassword:  WS_SERVER_PASSWD,
		OpsManagerUsername: OPS_MGR_USRN,
		OpsManagerPassword: OPS_MGR_PSWD,
	}

	metaConfig := readConfig(metaConfigFilePath)
	fmt.Println("Service Address is %s", metaConfig.ServerAddress)

	if metaConfig.ServerAddress != "" {
		fmt.Println("Service Address is %s", metaConfig.ServerAddress)
		meta.ServerAddress = metaConfig.ServerAddress
	}

	fmt.Println("TargetIdentifier is %s", metaConfig.TargetIdentifier)
	if metaConfig.TargetIdentifier != "" {
		meta.TargetIdentifier = metaConfig.TargetIdentifier
	}

	fmt.Println("NameOrAddress is %s", metaConfig.NameOrAddress)
	if metaConfig.NameOrAddress != "" {
		meta.NameOrAddress = metaConfig.NameOrAddress
	}
	if metaConfig.Username != "" {
		meta.Username = metaConfig.Username
	}
	if metaConfig.TargetType != "" {
		meta.TargetType = metaConfig.TargetType
	}
	if metaConfig.Password != "" {
		meta.Password = metaConfig.Password
	}

	return meta
}

func readConfig(path string) VMTMeta {
	file, e := ioutil.ReadFile(path)
	if e != nil {
		fmt.Printf("File error: %v\n", e)
		os.Exit(1)
	}
	fmt.Printf("%s\n", string(file))

	//m := new(Dispatch)
	//var m interface{}
	var metaData VMTMeta
	json.Unmarshal(file, &metaData)
	fmt.Printf("Results: %v\n", metaData)
	return metaData
}
