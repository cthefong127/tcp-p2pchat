package main

import (
	"syscall"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestRegisterPeers(t *testing.T) {
	var sendData []SendData
	sendTo := func(_ int, p []byte, _ int, to syscall.Sockaddr) error {
		sendData = append(sendData, SendData{
			Addr: to,
			Data: string(p),
		})
		return nil
	}
	registerPeers(
		map[string]*syscall.SockaddrInet4{
			"127.0.0.1:1000": &syscall.SockaddrInet4{Port: 1000, Addr: [4]byte{127, 0, 0, 1}},
		},
		&syscall.SockaddrInet4{Port: 2000, Addr: [4]byte{127, 0, 0, 2}},
		sendTo,
		-1,
	)
	if diff := cmp.Diff([]SendData{
		{
			Addr: &syscall.SockaddrInet4{Port: 1000, Addr: [4]byte{127, 0, 0, 1}},
			Data: "SERVER 127.0.0.2:2000",
		},
		{
			Addr: &syscall.SockaddrInet4{Port: 2000, Addr: [4]byte{127, 0, 0, 2}},
			Data: "CLIENT 127.0.0.1:1000",
		},
	}, sendData, cmpopts.IgnoreUnexported(syscall.SockaddrInet4{})); diff != "" {
		t.Errorf("registerPeers() mismatch (-want +got):\n%s", diff)
	}
}

type SendData struct {
	Addr syscall.Sockaddr
	Data string
}
