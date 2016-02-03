package vmturbo

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"

	"github.com/golang/glog"
)

// HostInterface contains all the kubelet methods required by the server.
// For testablitiy.
type HostInterface interface {
	GetAllTransactions() []*Transaction
}

// Server is a http.Handler which exposes kubelet functionality over HTTP.
type Server struct {
	counter *TransactionCounter
	mux     *http.ServeMux
}

// NewServer initializes and configures a kubelet.Server object to handle HTTP requests.
func NewServer(counter *TransactionCounter) Server {
	server := Server{
		counter: counter,
		mux:     http.NewServeMux(),
	}
	server.InstallDefaultHandlers()
	return server
}

// InstallDefaultHandlers registers the default set of supported HTTP request patterns with the mux.
func (s *Server) InstallDefaultHandlers() {
	s.mux.HandleFunc("/", handler)
	s.mux.HandleFunc("/transactions", s.getAllTransactions)
}

// ServeHTTP responds to HTTP requests on the Kubelet.
func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	s.mux.ServeHTTP(w, req)
}

func (s *Server) getAllTransactions(w http.ResponseWriter, r *http.Request) {
	transactions := s.counter.GetAllTransactions()
	// b, err := json.Marshal(transactions)

	// if err != nil {
	// 	panic(err)
	// }

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(transactions); err != nil {
		panic(err)
	}

	// w.Write(b)
}

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Vmturbo Kube-proxy Service.")
}

func ListenAndServeProxyServer(counter *TransactionCounter) {
	glog.Infof("start server")
	handler := NewServer(counter)
	s := &http.Server{
		Addr:           net.JoinHostPort("127.0.0.1", "2222"),
		Handler:        &handler,
		MaxHeaderBytes: 1 << 20,
	}
	glog.Fatal(s.ListenAndServe())
}
