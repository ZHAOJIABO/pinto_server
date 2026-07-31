package middleware

import (
	"context"
	"net"
	"net/http/httptest"
	"testing"

	"github.com/zhaojiabo/bobobeads_server/internal/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

func TestClientIPResolver_OnlyTrustsConfiguredProxyChain(t *testing.T) {
	resolver, err := NewClientIPResolver([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("NewClientIPResolver: %v", err)
	}

	t.Run("trusted proxy chain", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.test", nil)
		req.RemoteAddr = "10.0.0.8:443"
		req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.6")
		if got := resolver.Resolve(req); got != "203.0.113.7" {
			t.Errorf("Resolve() = %q, want %q", got, "203.0.113.7")
		}
	})

	t.Run("untrusted peer cannot spoof forwarded header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.test", nil)
		req.RemoteAddr = "198.51.100.9:443"
		req.Header.Set("X-Forwarded-For", "203.0.113.7")
		if got := resolver.Resolve(req); got != "198.51.100.9" {
			t.Errorf("Resolve() = %q, want %q", got, "198.51.100.9")
		}
	})
}

func TestClientIPInterceptor_OverridesClientSuppliedDeviceIP(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(clientIPMetadataKey, "203.0.113.7"))
	ctx = peer.NewContext(ctx, &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9090}})
	req := &pb.GuestLoginRequest{Header: &pb.RequestHeader{Device: &pb.Device{Ip: "198.51.100.9"}}}

	_, err := ClientIPInterceptor()(ctx, req, &grpc.UnaryServerInfo{}, func(_ context.Context, gotReq interface{}) (interface{}, error) {
		if got := gotReq.(*pb.GuestLoginRequest).GetHeader().GetDevice().GetIp(); got != "203.0.113.7" {
			t.Errorf("device.ip = %q, want trusted server IP", got)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("ClientIPInterceptor: %v", err)
	}
}

func TestClientIPInterceptor_IgnoresDirectClientMetadata(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(clientIPMetadataKey, "203.0.113.7"))
	ctx = peer.NewContext(ctx, &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP("198.51.100.9"), Port: 9090}})
	req := &pb.GuestLoginRequest{Header: &pb.RequestHeader{Device: &pb.Device{Ip: "192.0.2.1"}}}

	_, err := ClientIPInterceptor()(ctx, req, &grpc.UnaryServerInfo{}, func(_ context.Context, gotReq interface{}) (interface{}, error) {
		if got := gotReq.(*pb.GuestLoginRequest).GetHeader().GetDevice().GetIp(); got != "198.51.100.9" {
			t.Errorf("device.ip = %q, want direct peer IP", got)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("ClientIPInterceptor: %v", err)
	}
}

func TestClientIPResolver_InvalidCIDR(t *testing.T) {
	if _, err := NewClientIPResolver([]string{"not-a-cidr"}); err == nil {
		t.Fatal("NewClientIPResolver accepted invalid CIDR")
	}
}

func TestPeerIP(t *testing.T) {
	if got := peerIP(&net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 443}); got != "203.0.113.7" {
		t.Errorf("peerIP() = %q, want %q", got, "203.0.113.7")
	}
}
