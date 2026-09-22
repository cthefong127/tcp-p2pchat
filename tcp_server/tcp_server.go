package main

import (
	"fmt"
	"log"
	"syscall"
)

func main() {
	log.Println("Starting up tcp_server")

	// create a udp packet
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, 0)
	if err != nil { // if error not null then throw error
		panic(err)
	}
	defer syscall.Close(fd) // close the fd

	addr := &syscall.SockaddrInet4{
		Port: 5000,
	}

	// Binds UDP socket
	err = syscall.Bind(fd, addr)
	if err != nil {
		panic(err)
	}

	// create buffer of 1024 bytes
	buffer := make([]byte, 1024)

	// registry: in-memory list of clients connected so far
	registry := make(map[string]*syscall.SockaddrInet4)

	// repeatedly check for new data in buffer indefinitely (are there new messages?)
	for {
		// get the incoming socket's address length and the address itself
		_, clientAddr, err := syscall.Recvfrom(fd, buffer, 0)
		if err != nil {
			fmt.Println(err)
			continue
		}

		// ascertain the incoming socket is IPv4, ignore otherwise
		v4Addr, ok := clientAddr.(*syscall.SockaddrInet4)
		if !ok {
			fmt.Println("ignoring non-IPv4 sender")
			continue
		}

		if len(registry) == 0 {
			key := fmt.Sprintf("%d.%d.%d.%d:%d",
				v4Addr.Addr[0], v4Addr.Addr[1], v4Addr.Addr[2], v4Addr.Addr[3], v4Addr.Port)

			registry[key] = v4Addr
			fmt.Printf("Registered new peer: %s (waiting: %d)\n", key, len(registry))

			continue
		}
		registerPeers(registry, v4Addr, syscall.Sendto, fd)

	}
}

func registerPeers(registry map[string]*syscall.SockaddrInet4, v4Addr *syscall.SockaddrInet4, sendTo func(int, []byte, int, syscall.Sockaddr) error, fd int) {
	for key2, v4Addr2 := range registry {
		delete(registry, key2)
		key := fmt.Sprintf("%d.%d.%d.%d:%d",
			v4Addr.Addr[0], v4Addr.Addr[1], v4Addr.Addr[2], v4Addr.Addr[3], v4Addr.Port)

		if err := sendTo(fd, []byte("SERVER "+key), 0, v4Addr2); err != nil {
			fmt.Println("send err:", err)
			continue
		}

		if err := sendTo(fd, []byte("CLIENT "+key2), 0, v4Addr); err != nil {
			fmt.Println("send err:", err)
			continue
		}

		fmt.Printf("Introduced %s <-> %s\n", key, key2)
	}
}
