package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/fdbased"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
)

func main() {
	log.Println("Starting up tcp_client")

	// create a lil IPv4 UDP socket
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, 0)
	if err != nil {
		panic(err)
	}
	defer syscall.Close(fd) // close the file descriptor

	// step 1: register w/ bootstrap server using same fd
	// fd used for receiving PEER msg & talking to peer to keep local port constant
	// send the message to destination address
	if err := syscall.Sendto(fd, []byte("Hello from Go!"), 0, &syscall.SockaddrInet4{
		Port: 5000,
		Addr: [4]byte{34, 19, 101, 183},
	}); err != nil {
		panic(err)
	}

	// create buffer of 1024 bytes
	buffer := make([]byte, 1024)

	var peerAddr *syscall.SockaddrInet4
	isServer := false

	// step 2: wait for bootstrap server to introduce a peer
	// any packet not matching:
	//			"PEER <ip>:<port>"
	// is ignored for now (later implement retries/timeouts)
	// receive data from incoming packet within buffer

	for {
		n, from, err := syscall.Recvfrom(fd, buffer, 0)
		if err != nil {
			fmt.Println("could not receive bootstrap message", err)
			continue
		}

		v4Addr, ok := from.(*syscall.SockaddrInet4)
		if !ok {
			fmt.Println("ignoring non-IPv4 sender")
			continue
		}

		msg := string(buffer[:n]) // parse buffer as string
		fmt.Printf("Received %q from %d.%d.%d.%d.:%d\n", msg, v4Addr.Addr[0], v4Addr.Addr[1], v4Addr.Addr[2], v4Addr.Addr[3], v4Addr.Port)

		isServer = strings.HasPrefix(msg, "SERVER ")
		if !isServer && !strings.HasPrefix(msg, "CLIENT ") {
			fmt.Printf("invalid bootstrap response: %q\n", msg)
			continue
		}

		parsed, err := parsePeerAddr(strings.TrimPrefix(strings.TrimPrefix(msg, "CLIENT "), "SERVER "))
		if err != nil {
			fmt.Println("could not parse peer address:", err)
			continue
		}
		peerAddr = parsed
		fmt.Printf("Learned peer address: %d.%d.%d.%d:%d\n",
			peerAddr.Addr[0], peerAddr.Addr[1], peerAddr.Addr[2], peerAddr.Addr[3], peerAddr.Port)
		break
	}

	// step 3: peer connection established, no longer thru bootstrap server
	// "PUNCH" packet sent as placeholder for real hole punching, peer does same thing simultaneously
	// fine if first packet silently dropped by peer's NAT
	if err := syscall.Sendto(fd, []byte("PUNCH"), 0, peerAddr); err != nil {
		panic(err)
	}
	fmt.Println("Sent punch packet directly to peer")

	// step 3.5: set default addr for fdbased endpoint
	if err := syscall.Connect(fd, peerAddr); err != nil {
		log.Fatalln("cannot connect to peer address:", err)
	}

	// step 4: create fdbased stack
	// Create the stack and add a NIC.
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol},
	})

	const nicID = 1

	fdStack, err := fdbased.New(&fdbased.Options{
		FDs:                  []int{fd},
		MTU:                  1024,
		ProcessorsPerChannel: 1,
	})
	if err != nil {
		log.Fatalln("fdbased:", err)
	}

	if err := s.CreateNIC(nicID, fdStack); err != nil {
		log.Fatalln("CreateNIC:", err)
	}

	// Add default routes.
	s.SetRouteTable([]tcpip.Route{{
		Destination: header.IPv4EmptySubnet,
		NIC:         nicID,
	}})

	// Add address to NIC
	localAddress := tcpip.AddrFrom4Slice([]byte("\x7f\x00\x00\x02"))

	if err := s.AddProtocolAddress(nicID, tcpip.ProtocolAddress{
		Protocol: ipv4.ProtocolNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{
			Address:   localAddress,
			PrefixLen: 8,
		},
	}, stack.AddressProperties{}); err != nil {
		log.Fatalln("add protocol address:", err)
	}

	const (
		port     = 5000
		protocol = ipv4.ProtocolNumber
	)
	address := tcpip.AddrFrom4Slice([]byte("\x7f\x00\x00\x01"))
	fullAddress := tcpip.FullAddress{
		NIC:  nicID,
		Addr: address,
		Port: port,
	}

	// If server, start listening
	if isServer {
		l, err := gonet.ListenTCP(s, fullAddress, protocol)
		if err != nil {
			log.Fatalln("ListenTCP:", err)
		}
		conn, err := l.Accept()
		if err != nil {
			log.Fatalln("accept:", err)
		}
		handleConn(conn)
	} else { // Not server, client dials
		time.Sleep(5 * time.Second) // give the server time to listen
		ctx := context.Background()

		conn, err := gonet.DialContextTCP(ctx, s, fullAddress, protocol)
		if err != nil {
			log.Fatalln("dial:", err)
		}
		handleConn(conn)
	}
}

// handleConn:
func handleConn(conn net.Conn) {
	// Read loop
	go func() {
		recvBuf := make([]byte, 1024) // separate buffer since this now runs concurrently with the sender
		for {
			n, err := conn.Read(recvBuf)
			if err != nil {
				fmt.Println(err)
				continue
			}

			fmt.Printf("\nPeer says: %s\n> ", string(recvBuf[:n]))
		}
	}()
	// Write loop
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("> ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "" {
			continue // don't send empty lines
		}

		if _, err := conn.Write([]byte(input)); err != nil {
			panic(err)
		}
	}
}

// parsePeerAddr: turns an "ip:port" string (as sent by bootstrap server in
// a "PEER <ip:<port>" message) into a *syscall.SockaddrInet4 to pass to Sendto
func parsePeerAddr(s string) (*syscall.SockaddrInet4, error) {
	host, portStr, found := strings.Cut(s, ":")
	if !found {
		return nil, fmt.Errorf("invalid peer address %q: missing port", s)
	}

	octets := strings.Split(host, ".")
	if len(octets) != 4 {
		return nil, fmt.Errorf("invalid ip %q", host)
	}

	var addr [4]byte
	for i, o := range octets {
		v, err := strconv.Atoi(o)
		if err != nil || v < 0 || v > 255 {
			return nil, fmt.Errorf("invalid ip octet %q", o)
		}
		addr[i] = byte(v)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid port %q", portStr)
	}

	return &syscall.SockaddrInet4{Port: port, Addr: addr}, nil
}
