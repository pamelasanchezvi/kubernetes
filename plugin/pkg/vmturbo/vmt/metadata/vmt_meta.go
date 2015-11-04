package metadata

const (
	// Appliance address.
	// SERVER_ADDRESS string = "10.10.173.193:80"

	SERVER_ADDRESS string = "10.10.192.137:8080"

	TARGET_TYPE string = "Kubernetes"

	NAME_OR_ADDRESS string = "k8s_vmt"

	TARGET_IDENTIFIER string = "my_k8s"

	PASSWORD string = "fake_password"

	// Ops Manager related
	OPS_MGR_USRN = "administrator"
	OPS_MGR_PSWD = "a"
)

type VMTMeta struct {
	ServerAddress    string
	TargetType       string
	NameOrAddress    string
	TargetIdentifier string
	Password         string
}

func NewVMTMeta(serverAddr, targetType, nameOrAddress, targetIdentifier, password string) *VMTMeta {
	meta := &VMTMeta{
		ServerAddress:    SERVER_ADDRESS,
		TargetType:       TARGET_TYPE,
		NameOrAddress:    NAME_OR_ADDRESS,
		TargetIdentifier: TARGET_IDENTIFIER,
		Password:         PASSWORD,
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
