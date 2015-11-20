package metadata

const (
	// Appliance address.
	// SERVER_ADDRESS string = "10.10.200.114:8080"

	SERVER_ADDRESS string = "10.10.192.78:8080"

	// SERVER_ADDRESS string = "172.31.253.119:8080"

	TARGET_TYPE string = "Kubernetes"

	NAME_OR_ADDRESS string = "k8s_vmt"

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
	ServerAddress     string
	TargetType        string
	NameOrAddress     string
	TargetIdentifier  string
	Password          string
	LocalAddress      string
	WebSocketUsername string
	WebSocketPassword string
}

func NewVMTMeta(serverAddr, targetType, nameOrAddress, targetIdentifier, password string) *VMTMeta {
	meta := &VMTMeta{
		ServerAddress:     SERVER_ADDRESS,
		TargetType:        TARGET_TYPE,
		NameOrAddress:     NAME_OR_ADDRESS,
		TargetIdentifier:  TARGET_IDENTIFIER,
		Password:          PASSWORD,
		LocalAddress:      LOCAL_ADDRESS,
		WebSocketUsername: WS_SERVER_USRN,
		WebSocketPassword: WS_SERVER_PASSWD,
	}
	if serverAddr != "" {
		meta.ServerAddress = serverAddr
	}
	if targetIdentifier != "" {
		meta.TargetIdentifier = targetIdentifier
	}
	if nameOrAddress != "" {
		meta.NameOrAddress = nameOrAddress
	}
	if targetType != "" {
		meta.TargetType = targetType
	}
	if password != "" {
		meta.Password = password
	}

	return meta
}
