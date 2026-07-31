package middleware

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
)

const clientIPMetadataKey = "x-client-ip"

// ClientIPResolver resolves the client address while accepting forwarding
// headers only from explicitly trusted reverse proxies.
type ClientIPResolver struct {
	trustedProxies []*net.IPNet
}

func NewClientIPResolver(trustedProxyCIDRs []string) (ClientIPResolver, error) {
	resolver := ClientIPResolver{}
	for _, cidr := range trustedProxyCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return ClientIPResolver{}, fmt.Errorf("invalid trusted proxy CIDR %q: %w", cidr, err)
		}
		resolver.trustedProxies = append(resolver.trustedProxies, network)
	}
	return resolver, nil
}

// Resolve returns the closest untrusted hop in a trusted proxy chain. A peer
// outside trusted_proxy_cidrs can never supply a forwarding header.
func (r ClientIPResolver) Resolve(req *http.Request) string {
	remoteIP := hostIP(req.RemoteAddr)
	if remoteIP == nil {
		return ""
	}
	if !r.isTrusted(remoteIP) {
		return remoteIP.String()
	}

	forwarded := forwardedIPs(req.Header.Values("X-Forwarded-For"))
	if len(forwarded) == 0 {
		if realIP := net.ParseIP(strings.TrimSpace(req.Header.Get("X-Real-IP"))); realIP != nil {
			return realIP.String()
		}
		return remoteIP.String()
	}

	forwarded = append(forwarded, remoteIP)
	for i := len(forwarded) - 1; i >= 0; i-- {
		if !r.isTrusted(forwarded[i]) {
			return forwarded[i].String()
		}
	}
	return remoteIP.String()
}

func (r ClientIPResolver) isTrusted(ip net.IP) bool {
	for _, network := range r.trustedProxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func forwardedIPs(headers []string) []net.IP {
	var ips []net.IP
	for _, header := range headers {
		for _, value := range strings.Split(header, ",") {
			if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil {
				ips = append(ips, ip)
			}
		}
	}
	return ips
}

func hostIP(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	return net.ParseIP(host)
}

func peerIP(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	if ip := hostIP(addr.String()); ip != nil {
		return ip.String()
	}
	return ""
}

// ForwardClientIP resolves the IP before grpc-gateway turns the HTTP request
// into a local gRPC call. The generated header is overwritten on every request.
func ForwardClientIP(resolver ClientIPResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			req.Header.Set("X-Client-IP", resolver.Resolve(req))
			next.ServeHTTP(w, req)
		})
	}
}

// ClientIPInterceptor makes the server-resolved address authoritative over any
// client-supplied device.ip before a request reaches application handlers.
func ClientIPInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		ip := gatewayClientIP(ctx)
		if ip == "" {
			ip = clientPeerIP(ctx)
		}
		if ip != "" {
			setRequestDeviceIP(req, ip)
		}
		return handler(ctx, req)
	}
}

func gatewayClientIP(ctx context.Context) string {
	ip := metadataValue(ctx, clientIPMetadataKey)
	if net.ParseIP(ip) == nil {
		return ""
	}
	p, ok := peer.FromContext(ctx)
	if !ok {
		return ""
	}
	gatewayIP := net.ParseIP(peerIP(p.Addr))
	if gatewayIP == nil || !gatewayIP.IsLoopback() {
		return ""
	}
	return ip
}

func clientPeerIP(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return ""
	}
	return peerIP(p.Addr)
}

func setRequestDeviceIP(req interface{}, ip string) {
	withHeader, ok := req.(interface{ GetHeader() *pb.RequestHeader })
	if !ok || withHeader.GetHeader() == nil {
		return
	}
	header := withHeader.GetHeader()
	if header.Device == nil {
		header.Device = &pb.Device{}
	}
	header.Device.Ip = ip
}
