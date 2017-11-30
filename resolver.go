package kuberesolver

import (
	"net"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/resolver"
)

const (
	scheme = "k8s"
)

type k8sResolverBuilder struct {
	client    *k8sClient
	namespace string
}

func (b *k8sResolverBuilder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOption) (resolver.Resolver, error) {
	result := make(chan Event)
	stop := make(chan struct{})

	servicePort := strings.Split(target.Endpoint, ":")
	var port int64

	if len(servicePort) > 1 {
		var err error
		if port, err = strconv.ParseInt(servicePort[1], 10, 64); err != nil {
			grpclog.Fatalf("Invalid port format: %s", servicePort[1])
		}
	}

	go func() {
		for {
			if err := b.client.startServiceEndpointUpdateWatcher(b.namespace, servicePort[0], result, stop); err != nil {
				grpclog.Errorf("kubernetes resolver: watcher ended with error %s. retrying in 500ms\n", err.Error())
				time.Sleep(time.Millisecond * 500)
			} else {
				// NOTE(Olivier): If the function returns a nil value, this means it exited gracefully
				// therefore, we end the retry loop
				return
			}
		}
	}()

	r := &k8sResolver{
		service:   servicePort[0],
		port:      port,
		cc:        cc,
		endpoints: make([]resolver.Address, 0),
		result:    result,
		stop:      stop,
	}
	r.Start()

	return r, nil
}

func (b *k8sResolverBuilder) Scheme() string {
	return scheme
}

// NewResolver returns a new k8s resolver builder to a given namespace
func NewResolver(namespace string) (resolver.Builder, error) {
	client, err := newK8sClient()
	if err != nil {
		return nil, err
	}
	return &k8sResolverBuilder{
		client:    client,
		namespace: namespace,
	}, nil
}

type k8sResolver struct {
	service   string
	port      int64
	cc        resolver.ClientConn
	endpoints []resolver.Address
	result    chan Event
	stop      chan struct{}
	started   bool
}

func (r *k8sResolver) Start() {
	for event := range r.result {
		nextEndpoints := make([]resolver.Address, 0)
		for _, subset := range event.Object.Subsets {
			port := "80"

			if r.port > 0 {
				port = strconv.FormatInt(r.port, 10)
			} else {

				if len(subset.Ports) > 0 {
					// If there is more than 1 port, look for one named "grpc"
					if len(subset.Ports) > 1 {
						found := false
						for _, p := range subset.Ports {
							if p.Name == "grpc" {
								found = true
								port = strconv.Itoa(p.Port)
								break
							}
						}
						if !found {
							port = strconv.Itoa(subset.Ports[0].Port)
						}
					} else {
						port = strconv.Itoa(subset.Ports[0].Port)
					}
				}
			}

			if len(subset.Ports) > 0 {
				port = strconv.Itoa(subset.Ports[0].Port)
			}

			for _, addr := range subset.Addresses {
				nextEndpoints = append(nextEndpoints, resolver.Address{
					Addr: net.JoinHostPort(addr.IP, port),
					Type: resolver.Backend,
				})
			}
		}
		grpclog.Infof("New endpoints for service %s: %v", r.service, nextEndpoints)
		r.cc.NewAddress(nextEndpoints)
		r.endpoints = nextEndpoints
	}
}

func (r *k8sResolver) ResolveNow(options resolver.ResolveNowOption) {}

// Close closes the resolver.
func (r *k8sResolver) Close() {
	r.stop <- struct{}{}
}
