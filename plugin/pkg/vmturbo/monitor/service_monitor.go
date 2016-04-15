package monitor

import (
	"fmt"

	vmtproxy "k8s.io/kubernetes/pkg/proxy/vmturbo"
	vmtAdvisor "k8s.io/kubernetes/plugin/pkg/vmturbo/cadvisor"
)

type ServiceMonitor struct{}

func (self *ServiceMonitor) GetServiceTransactions(host vmtAdvisor.Host) (transactions []vmtproxy.Transaction, err error) {
	url := fmt.Sprintf("http://%s:%d/", host.IP, 2222)
	client, err := NewServiceMonitorClient(url)
	if err != nil {
		return nil, fmt.Errorf("Failed to create service monitor client: %s", err)
	}
	transactions, err = client.TransactionInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to get stats from K8s service network monitor %q - %v\n", url, err)
	}
	return
}
