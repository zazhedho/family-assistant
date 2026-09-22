package main

import (
	"net"
	"testing"
)

func TestFailOnErrorWithNilError(t *testing.T) {
	FailOnError(nil, "should not fail")
}

func TestFindIPv4AddressSkipsLoopbackAndIPv6(t *testing.T) {
	addresses := []net.Addr{
		&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(32, 32)},
		&net.IPNet{IP: net.ParseIP("2001:db8::1"), Mask: net.CIDRMask(128, 128)},
		&net.IPNet{IP: net.ParseIP("192.0.2.10"), Mask: net.CIDRMask(32, 32)},
	}

	if got := findIPv4Address(addresses); got != "192.0.2.10" {
		t.Fatalf("findIPv4Address() = %q, want %q", got, "192.0.2.10")
	}
}

func TestFindIPv4AddressReturnsUnknownWithoutUsableAddress(t *testing.T) {
	if got := findIPv4Address([]net.Addr{
		&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(32, 32)},
	}); got != "unknown" {
		t.Fatalf("findIPv4Address() = %q, want %q", got, "unknown")
	}
}
