package vmt

import (
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client"
	"k8s.io/kubernetes/pkg/labels"

	vmtapi "k8s.io/kubernetes/plugin/pkg/vmturbo/vmt/api"
	vmtmeta "k8s.io/kubernetes/plugin/pkg/vmturbo/vmt/metadata"

	comm "github.com/vmturbo/vmturbo-go-sdk/communicator"
	"github.com/vmturbo/vmturbo-go-sdk/sdk"

	"github.com/golang/glog"
)

// impletements sdk.ServerMessageHandler
type KubernetesServerMessageHandler struct {
	kubeClient *client.Client
	meta       *vmtmeta.VMTMeta
	wsComm     *comm.WebSocketCommunicator
}

// Use the vmt restAPI to add a Kubernetes target.
func (handler *KubernetesServerMessageHandler) AddTarget() {
	vmtUrl := handler.wsComm.VmtServerAddress

	extCongfix := make(map[string]string)
	extCongfix["Username"] = vmtmeta.OPS_MGR_USRN
	extCongfix["Password"] = vmtmeta.OPS_MGR_PSWD
	vmturboApi := vmtapi.NewVmtApi(vmtUrl, extCongfix)

	// Add Kubernetes target.
	// targetType, nameOrAddress, targetIdentifier, password
	vmturboApi.AddK8sTarget(handler.meta.TargetType, handler.meta.NameOrAddress, handler.meta.TargetIdentifier, handler.meta.Password)
}

// send an API request to make server start a discovery process on current k8s.
func (handler *KubernetesServerMessageHandler) DiscoverTarget() {
	vmtUrl := handler.wsComm.VmtServerAddress

	extCongfix := make(map[string]string)
	extCongfix["Username"] = vmtmeta.OPS_MGR_USRN
	extCongfix["Password"] = vmtmeta.OPS_MGR_PSWD
	vmturboApi := vmtapi.NewVmtApi(vmtUrl, extCongfix)

	// Discover Kubernetes target.
	vmturboApi.DiscoverTarget(handler.meta.NameOrAddress)
}

// If server sends a validation request, validate the request.
// TODO, for now k8s validate all the request. aka, no matter what usr/passwd is provided, always pass validation.
// The correct bahavior is to set ErrorDTO when validation fails.
func (handler *KubernetesServerMessageHandler) Validate(serverMsg *comm.MediationServerMessage) {
	//Always send Validated for now
	glog.V(3).Infof("Kubernetes validation request from Server")

	// 1. Get message ID.
	messageID := serverMsg.GetMessageID()
	// 2. Build validationResponse.
	validationResponse := new(comm.ValidationResponse)
	// 3. Create client message with ClientMessageBuilder.
	clientMsg := comm.NewClientMessageBuilder(messageID).SetValidationResponse(validationResponse).Create()
	handler.wsComm.SendClientMessage(clientMsg)

	// TODO: Need to sleep some time, waiting validated. Or we should add reponse msg from server.
	time.Sleep(100 * time.Millisecond)
	handler.DiscoverTarget()
}

// DiscoverTopology receives a discovery request from server and start probing the k8s.
func (handler *KubernetesServerMessageHandler) DiscoverTopology(serverMsg *comm.MediationServerMessage) {
	//Discover the kubernetes topology
	glog.V(3).Infof("Discover topology request from server.")

	// 1. Get message ID
	messageID := serverMsg.GetMessageID()

	// 2. Build discoverResponse
	// must have kubeClient to do ParseNode and ParsePod
	if handler.kubeClient == nil {
		glog.V(3).Infof("kubenetes client is nil, error")
		return
	}

	kubeProbe := &KubeProbe{
		kubeClient: handler.kubeClient,
	}

	nodeEntityDtos, err := kubeProbe.ParseNode()
	if err != nil {
		// TODO, should here still send out msg to server?
		return
	}

	podEntityDtos, err := kubeProbe.ParsePod(api.NamespaceAll)
	if err != nil {
		// TODO, should here still send out msg to server? Or set errorDTO?
		return
	}

	appEntityDtos, err := kubeProbe.ParseApplication(api.NamespaceAll)
	if err != nil {
		return
	}

	serviceEntityDtos, err := kubeProbe.ParseService(api.NamespaceAll, labels.Everything())
	if err != nil {
		// TODO, should here still send out msg to server? Or set errorDTO?
		return
	}

	entityDtos := nodeEntityDtos
	entityDtos = append(entityDtos, podEntityDtos...)
	entityDtos = append(entityDtos, appEntityDtos...)
	entityDtos = append(entityDtos, serviceEntityDtos...)
	discoveryResponse := &comm.DiscoveryResponse{
		EntityDTO: entityDtos,
	}

	// 3. Build Client message
	clientMsg := comm.NewClientMessageBuilder(messageID).SetDiscoveryResponse(discoveryResponse).Create()

	handler.wsComm.SendClientMessage(clientMsg)
}

// Receives an action request from server and call ActionExecutor to execute action.
func (handler *KubernetesServerMessageHandler) HandleAction(serverMsg *comm.MediationServerMessage) {
	messageID := serverMsg.GetMessageID()
	actionRequest := serverMsg.GetActionRequest()
	// In the kubernetes case, ProbeType and AccountValue check is not necessary here since
	// the mediation client (vmturbo service) is embeded inside kubernetes.
	actionItemDTO := actionRequest.GetActionItemDTO()
	glog.V(3).Infof("The received ActionItemDTO is %v", actionItemDTO)
	actionExecutor := &KubernetesActionExecutor{
		kubeClient: handler.kubeClient,
	}
	actionExecutor.ExcuteAction(actionItemDTO, messageID)
}

type VMTCommunicator struct {
	kubeClient *client.Client
	meta       *vmtmeta.VMTMeta
	wsComm     *comm.WebSocketCommunicator
}

func NewVMTCommunicator(client *client.Client, vmtMetadata *vmtmeta.VMTMeta) *VMTCommunicator {
	return &VMTCommunicator{
		kubeClient: client,
		meta:       vmtMetadata,
	}
}

func (vmtcomm *VMTCommunicator) Run() {
	vmtcomm.Init()
	vmtcomm.RegisterKubernetes()
}

// Init() intialize the VMTCommunicator, creating websocket communicator and server message handler.
func (vmtcomm *VMTCommunicator) Init() {
	wsCommunicator := &comm.WebSocketCommunicator{
		VmtServerAddress: vmtcomm.meta.ServerAddress,
		LocalAddress:     vmtcomm.meta.LocalAddress,
		ServerUsername:   vmtcomm.meta.WebSocketUsername,
		ServerPassword:   vmtcomm.meta.WebSocketPassword,
	}
	vmtcomm.wsComm = wsCommunicator

	// First create the message handler for kubernetes
	kubeMsgHandler := &KubernetesServerMessageHandler{
		kubeClient: vmtcomm.kubeClient,
		meta:       vmtcomm.meta,
		wsComm:     wsCommunicator,
	}
	wsCommunicator.ServerMsgHandler = kubeMsgHandler
	return
}

// Register Kubernetes target onto server and start listen to websocket.
func (vmtcomm *VMTCommunicator) RegisterKubernetes() {
	// 1. Construct the account definition for kubernetes.
	acctDefProps := createAccountDefKubernetes()

	// 2. Build supply chain.
	templateDtos := createSupplyChain()
	glog.V(3).Infof("Supply chain for Kubernetes is created.")

	// 3. construct the kubernetesProbe, the only probe supported.
	probeType := vmtcomm.meta.TargetType
	probeCat := "Container"
	kubernateProbe := comm.NewProbeInfoBuilder(probeType, probeCat, templateDtos, acctDefProps).Create()

	// 4. Add kubernateProbe to probe, and that's the only probe supported in this client.
	var probes []*comm.ProbeInfo
	probes = append(probes, kubernateProbe)

	// 5. Create mediation container
	containerInfo := &comm.ContainerInfo{
		Probes: probes,
	}

	messageID := int32(1)

	// 6. build client message.
	clientMsg := comm.NewClientMessageBuilder(messageID).SetContainerInfo(containerInfo).Create()

	vmtcomm.wsComm.RegisterAndListen(clientMsg)
}

// TODO, rephrase comment.
// create account definition for kubernetes, which is used later to create Kubernetes probe.
// The return type is a list of ProbeInfo_AccountDefProp.
// For a valid definition, targetNameIdentifier, username and password should be contained.
func createAccountDefKubernetes() []*comm.AccountDefEntry {
	var acctDefProps []*comm.AccountDefEntry

	// target id
	targetIDAcctDefEntry := comm.NewAccountDefEntryBuilder("targetIdentifier", "Address",
		"IP of the kubernetes master", ".*", comm.AccountDefEntry_OPTIONAL, false).Create()
	// targetIdEntryKey := "targetIdentifier"
	// targetIdAcctDefProp := &comm.AccountDefEntry{
	// 	Key:   &targetIdEntryKey,
	// 	Value: targetIDAcctDefEntry,
	// }
	acctDefProps = append(acctDefProps, targetIDAcctDefEntry)

	// username
	usernameAcctDefEntry := comm.NewAccountDefEntryBuilder("username", "Username",
		"Username of the kubernetes master", ".*", comm.AccountDefEntry_OPTIONAL, false).Create()
	// usernameEntryKey := "username"
	// usernameAcctDefProp := &comm.AccountDefEntry{
	// 	Key:   &usernameEntryKey,
	// 	Value: usernameAcctDefEntry,
	// }
	acctDefProps = append(acctDefProps, usernameAcctDefEntry)

	// password
	passwdAcctDefEntry := comm.NewAccountDefEntryBuilder("password", "Password",
		"Password of the kubernetes master", ".*", comm.AccountDefEntry_OPTIONAL, true).Create()
	// passwdEntryKey := "password"
	// passwdAcctDefProp := &comm.AccountDefEntry{
	// 	Key:   &passwdEntryKey,
	// 	Value: passwdAcctDefEntry,
	// }
	acctDefProps = append(acctDefProps, passwdAcctDefEntry)

	return acctDefProps
}

// also include kubernetes supply chain explanation
func createSupplyChain() []*sdk.TemplateDTO {
	glog.V(3).Infof(".......... Now use builder to create a supply chain ..........")

	minionSupplyChainNodeBuilder := sdk.NewSupplyChainNodeBuilder()
	minionSupplyChainNodeBuilder = minionSupplyChainNodeBuilder.
		Entity(sdk.EntityDTO_VIRTUAL_MACHINE).
		Selling(sdk.CommodityDTO_CPU_ALLOCATION).
		Selling(sdk.CommodityDTO_MEM_ALLOCATION).
		Selling(sdk.CommodityDTO_VCPU).
		Selling(sdk.CommodityDTO_VMEM)
	glog.V(3).Infof(".......... minion supply chain node builder is created ..........")

	// Pod Supplychain builder
	podSupplyChainNodeBuilder := sdk.NewSupplyChainNodeBuilder()
	podSupplyChainNodeBuilder = podSupplyChainNodeBuilder.
		Entity(sdk.EntityDTO_CONTAINER_POD).
		Selling(sdk.CommodityDTO_CPU_ALLOCATION).
		Selling(sdk.CommodityDTO_MEM_ALLOCATION)

	emptyKey := ""
	cpuAllocationType := sdk.CommodityDTO_CPU_ALLOCATION
	cpuAllocationTemplateComm := &sdk.TemplateCommodity{
		Key:           &emptyKey,
		CommodityType: &cpuAllocationType,
	}
	memAllocationType := sdk.CommodityDTO_MEM_ALLOCATION
	memAllocationTemplateComm := &sdk.TemplateCommodity{
		Key:           &emptyKey,
		CommodityType: &memAllocationType,
	}

	podSupplyChainNodeBuilder = podSupplyChainNodeBuilder.
		Provider(sdk.EntityDTO_VIRTUAL_MACHINE, sdk.Provider_LAYERED_OVER).
		Buys(*cpuAllocationTemplateComm).
		Buys(*memAllocationTemplateComm)
	glog.V(3).Infof(".......... pod supply chain node builder is created ..........")

	// Application supplychain builder
	appSupplyChainNodeBuilder := sdk.NewSupplyChainNodeBuilder()
	appSupplyChainNodeBuilder = appSupplyChainNodeBuilder.
		Entity(sdk.EntityDTO_APPLICATION).
		Selling(sdk.CommodityDTO_TRANSACTION)
	// Buys CpuAllocation/MemAllocation from Pod
	appCpuAllocationTemplateComm := &sdk.TemplateCommodity{
		Key:           &emptyKey,
		CommodityType: &cpuAllocationType,
	}
	appMemAllocationTemplateComm := &sdk.TemplateCommodity{
		Key:           &emptyKey,
		CommodityType: &memAllocationType,
	}
	appSupplyChainNodeBuilder = appSupplyChainNodeBuilder.Provider(sdk.EntityDTO_CONTAINER_POD, sdk.Provider_LAYERED_OVER).Buys(*appCpuAllocationTemplateComm).Buys(*appMemAllocationTemplateComm)
	// Buys VCpu and VMem from VM
	vCpuType := sdk.CommodityDTO_VCPU
	appVCpu := &sdk.TemplateCommodity{
		Key:           &emptyKey,
		CommodityType: &vCpuType,
	}
	vMemType := sdk.CommodityDTO_VMEM
	appVMem := &sdk.TemplateCommodity{
		Key:           &emptyKey,
		CommodityType: &vMemType,
	}
	appSupplyChainNodeBuilder = appSupplyChainNodeBuilder.Provider(sdk.EntityDTO_VIRTUAL_MACHINE, sdk.Provider_HOSTING).Buys(*appVCpu).Buys(*appVMem)

	// Application supplychain builder
	vAppSupplyChainNodeBuilder := sdk.NewSupplyChainNodeBuilder()
	vAppSupplyChainNodeBuilder = vAppSupplyChainNodeBuilder.
		Entity(sdk.EntityDTO_VIRTUAL_APPLICATION)

	transactionType := sdk.CommodityDTO_TRANSACTION

	// Buys CpuAllocation/MemAllocation from Pod
	transactionTemplateComm := &sdk.TemplateCommodity{
		Key:           &emptyKey,
		CommodityType: &transactionType,
	}
	vAppSupplyChainNodeBuilder = vAppSupplyChainNodeBuilder.Provider(sdk.EntityDTO_APPLICATION, sdk.Provider_LAYERED_OVER).Buys(*transactionTemplateComm)

	// Link from Application to VM
	extLinkBuilder := sdk.NewExternalEntityLinkBuilder()
	extLinkBuilder.Link(sdk.EntityDTO_CONTAINER_POD, sdk.EntityDTO_VIRTUAL_MACHINE, sdk.Provider_LAYERED_OVER).
		Commodity(cpuAllocationType).
		Commodity(memAllocationType).
		ProbeEntityPropertyDef(sdk.SUPPLYCHAIN_CONSTANT_IP_ADDRESS, "IP Address where the Application is running").
		ExternalEntityPropertyDef(sdk.VM_IP)

	vmExternalLink := extLinkBuilder.Build()

	supplyChainBuilder := sdk.NewSupplyChainBuilder()
	supplyChainBuilder.Top(vAppSupplyChainNodeBuilder)
	supplyChainBuilder.Entity(appSupplyChainNodeBuilder)
	supplyChainBuilder.Entity(podSupplyChainNodeBuilder)
	supplyChainBuilder.ConnectsTo(vmExternalLink)
	supplyChainBuilder.Entity(minionSupplyChainNodeBuilder)

	return supplyChainBuilder.Create()
}

// Send action response to vmt server.
func (vmtcomm *VMTCommunicator) SendActionReponse(state sdk.ActionResponseState, progress, messageID int32, description string) {
	// 1. build response
	response := &comm.ActionResponse{
		ActionResponseState: &state,
		Progress:            &progress,
		ResponseDescription: &description,
	}

	// 2. built action result.
	result := &comm.ActionResult{
		Response: response,
	}

	// 3. Build Client message
	clientMsg := comm.NewClientMessageBuilder(messageID).SetActionResponse(result).Create()

	vmtcomm.wsComm.SendClientMessage(clientMsg)
}

// Code is very similar to those in serverMsgHandler.
func (vmtcomm *VMTCommunicator) DiscoverTarget() {
	vmtUrl := vmtcomm.wsComm.VmtServerAddress

	extCongfix := make(map[string]string)
	extCongfix["Username"] = "administrator"
	extCongfix["Password"] = "a"
	vmturboApi := vmtapi.NewVmtApi(vmtUrl, extCongfix)

	// Discover Kubernetes target.
	vmturboApi.DiscoverTarget(vmtcomm.meta.NameOrAddress)
}
