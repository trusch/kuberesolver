# kuberesolver

gRPC client side load balancer with k8s service resolver.

Inspired from [sercand/kuberesolver](https://github.com/sercand/kuberesolver). The main difference is the support for grpc-go's new load balancing api.
See [spec](https://github.com/grpc/proposal/pull/30) and main [issue](https://github.com/grpc/grpc-go/issues/1388)
