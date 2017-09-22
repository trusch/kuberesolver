package kuberesolver

import (
	"os"
	"fmt"
	"io/ioutil"
	"crypto/x509"
	"crypto/tls"
	"time"
	"net/http"
	"net"
	"encoding/json"
	"sync"
	"google.golang.org/grpc/grpclog"
)

const (
	serviceAccountToken  = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	serviceAccountCACert = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

type k8sClient struct {
	host string
	token string
	httpClient *http.Client
}

func (c *k8sClient) startServiceEndpointUpdateWatcher(namespace string, service string, results chan<- Event, stop <-chan struct{}) error {
	url := fmt.Sprintf("%s/api/v1/watch/namespaces/%s/endpoints/%s", c.host, namespace, service)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	req.Header.Add("Authorization", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Invalid status code: %d", resp.StatusCode)
	}

	decoder := json.NewDecoder(resp.Body)

	mu := sync.Mutex{}
	requestedStop := false

	go func() {
		<-stop
		mu.Lock()
		defer mu.Unlock()

		requestedStop = true
		grpclog.Infof("kubernetes resolver: stopping update stream for service %s\n", service)
		resp.Body.Close()
		close(results)
	}()

	for {
		var ev Event

		if err := decoder.Decode(&ev); err != nil {
			mu.Lock()
			shouldQuit := requestedStop
			mu.Unlock()
			if shouldQuit {
				//NOTE(Olivier): Omit the error since it will be because we closed the stream
				return nil
			}
			return err
		} else {
			results <- ev
		}
	}
}


func newK8sClient() (*k8sClient, error) {
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if len(host) == 0 || len(port) == 0 {
		return nil, fmt.Errorf("unable to load in-cluster configuration, KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT must be defined")
	}
	token, err := ioutil.ReadFile(serviceAccountToken)
	if err != nil {
		return nil, err
	}
	ca, err := ioutil.ReadFile(serviceAccountCACert)
	if err != nil {
		return nil, err
	}
	certPool := x509.NewCertPool()
	certPool.AppendCertsFromPEM(ca)
	transport := &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS10,
		RootCAs:    certPool,
	}}
	httpClient := &http.Client{Transport: transport, Timeout: time.Nanosecond * 0}

	return &k8sClient{
		host: "https://" + net.JoinHostPort(host, port),
		token: "Bearer " + string(token),
		httpClient: httpClient,
	}, nil
}
