# tcp-p2p

Peer-to-peer chat application with NAT traversal. Sends messages over end-to-end peer-to-peer TCP connections using the gVisor userspace TCP stack. Encapsulates TCP packets inside of UDP packets. Bootstrap server made with custom UDP text-based protocol. Includes unit testing.