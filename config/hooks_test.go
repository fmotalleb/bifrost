package config

import (
	"net"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-viper/mapstructure/v2"
)

func executeHook(
	t *testing.T,
	hook mapstructure.DecodeHookFunc,
	from any,
	toType reflect.Type,
) (any, error) {
	t.Helper()
	return mapstructure.DecodeHookExec(
		hook,
		reflect.ValueOf(from),
		reflect.New(toType).Elem(),
	)
}

func TestStringToNetAddrPortHook(t *testing.T) {
	hook := StringToNetAddrPortHook()

	out, err := executeHook(t, hook, "8080", reflect.TypeOf(netip.AddrPort{}))
	if err != nil {
		t.Fatalf("hook returned error: %v", err)
	}
	addr := out.(netip.AddrPort)
	if got := addr.String(); got != "127.0.0.1:8080" {
		t.Fatalf("unexpected addrport: %q", got)
	}

	_, err = executeHook(t, hook, "not-valid", reflect.TypeOf(netip.AddrPort{}))
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestStringToNetAddrHook(t *testing.T) {
	hook := StringToNetAddrHook()

	out, err := executeHook(t, hook, "8.8.8.8", reflect.TypeOf(net.IP{}))
	if err != nil {
		t.Fatalf("hook returned error: %v", err)
	}
	ip := out.(net.IP)
	if got := ip.String(); got != "8.8.8.8" {
		t.Fatalf("unexpected ip: %q", got)
	}

	_, err = executeHook(t, hook, "bad-ip", reflect.TypeOf(net.IP{}))
	if err == nil || !strings.Contains(err.Error(), "failed to parse input") {
		t.Fatalf("expected invalid ip error, got: %v", err)
	}
}

func TestIntToNetAddrPortHook(t *testing.T) {
	hook := IntToNetAddrPortHook()
	out, err := executeHook(t, hook, int(1080), reflect.TypeOf(netip.AddrPort{}))
	if err != nil {
		t.Fatalf("hook returned error: %v", err)
	}
	addr := out.(netip.AddrPort)
	if got := addr.String(); got != "127.0.0.1:1080" {
		t.Fatalf("unexpected addrport: %q", got)
	}
}

func TestStringToDurationHook(t *testing.T) {
	hook := StringToDurationHook()
	out, err := executeHook(t, hook, "1500ms", reflect.TypeOf(time.Duration(0)))
	if err != nil {
		t.Fatalf("hook returned error: %v", err)
	}
	dur := out.(time.Duration)
	if dur != 1500*time.Millisecond {
		t.Fatalf("unexpected duration: %v", dur)
	}

	_, err = executeHook(t, hook, "x", reflect.TypeOf(time.Duration(0)))
	if err == nil || !strings.Contains(err.Error(), "parse duration") {
		t.Fatalf("expected parse duration error, got: %v", err)
	}
}
