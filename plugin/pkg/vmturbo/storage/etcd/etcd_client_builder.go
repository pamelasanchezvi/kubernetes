package etcd

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"k8s.io/kubernetes/pkg/client"
	"k8s.io/kubernetes/pkg/tools"

	forked "k8s.io/kubernetes/third_party/forked/coreos/go-etcd/etcd"

	"github.com/coreos/go-etcd/etcd"
	"github.com/golang/glog"
)

type EtcdClientBuilder struct {
	etcdConfigFile string
	etcdServerList []string
	transport      *http.Transport
}

func NewEtcdClientBuilder() *EtcdClientBuilder {
	return new(EtcdClientBuilder)
}

func (this *EtcdClientBuilder) Config(etcdConfig string) *EtcdClientBuilder {
	this.etcdConfigFile = etcdConfig

	return this
}

func (this *EtcdClientBuilder) ServerList(servers []string) *EtcdClientBuilder {
	this.etcdServerList = servers

	return this
}

func (this *EtcdClientBuilder) SetTransport(ca, certfile, keyfile string) *EtcdClientBuilder {
	transport, _ := generateTransport(ca, certfile, keyfile)
	this.transport = transport

	return this
}

func generateTransport(ca, certfile, keyfile string) (*http.Transport, error) {
	if ca == "" || certfile == "" || keyfile == "" {
		return &http.Transport{
			Dial: forked.Dial,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
			MaxIdleConnsPerHost: 500,
		}, nil
	} else {
		tlsConfig, err := client.TLSConfigFor(&client.Config{
			TLSClientConfig: client.TLSClientConfig{
				CertFile: certfile,
				KeyFile:  keyfile,
				CAFile:   ca,
			},
		})
		if err != nil {
			glog.Errorf("Error creating tls config: %s", err)
			return nil, err
		}

		return setTransportDefaults(&http.Transport{
			TLSClientConfig: tlsConfig,
			Dial:            forked.Dial,
			// Because watches are very bursty, defends against long delays in watch reconnections.
			MaxIdleConnsPerHost: 500,
		}), nil
	}
}

var defaultTransport = http.DefaultTransport.(*http.Transport)

// setTransportDefaults applies the defaults from http.DefaultTransport
// for the Proxy, Dial, and TLSHandshakeTimeout fields if unset
func setTransportDefaults(t *http.Transport) *http.Transport {
	if t.Proxy == nil {
		t.Proxy = defaultTransport.Proxy
	}
	if t.Dial == nil {
		t.Dial = defaultTransport.Dial
	}
	if t.TLSHandshakeTimeout == 0 {
		t.TLSHandshakeTimeout = defaultTransport.TLSHandshakeTimeout
	}
	return t
}

func (this *EtcdClientBuilder) Create() (client tools.EtcdClient, err error) {
	if this.etcdConfigFile != "" {
		client, err = etcd.NewClientFromFile(this.etcdConfigFile)
		if err != nil {
			return
		}
	} else {
		etcdClient := etcd.NewClient(this.etcdServerList)
		etcdClient.SetTransport(this.transport)
		client = etcdClient
	}
	return
}

func (this *EtcdClientBuilder) CreateAndTest() (client tools.EtcdClient, err error) {
	client, err = this.Create()
	if err != nil {
		glog.Errorf("Failed to create etcd client: %s", err)
		return
	}
	err = testEtcdClient(client)
	if err != nil {
		glog.Errorf("Failed to pass etcd client test: %s", err)
	}
	return
}

// TestEtcdClient verifies a client is functional.  It will attempt to
// connect to the etcd server and block until the server responds at least once, or return an
// error if the server never responded.
func testEtcdClient(etcdClient tools.EtcdClient) error {
	for i := 0; ; i++ {
		_, err := etcdClient.Get("/", false, false)
		if err == nil {
			break
		}
		if i > 100 {
			return fmt.Errorf("Could not reach etcd: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	glog.V(3).Infof("Etcd client test passed")
	return nil
}
