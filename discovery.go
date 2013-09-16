//
// Handles discovery of airplay devices on the local network. That means
// that this is basically a multicast DNS and service discovery client,
// except that we only implement the bare minimum to do airplay devices.
//
// Relevant RFCs:
// http://www.ietf.org/rfc/rfc6762.txt - Multicast DNS
// http://www.ietf.org/rfc/rfc6763.txt - DNS-Based Service Discovery
//
// http://nto.github.io/AirPlay.html - Unofficial AirPlay Protocol Specification
//

package main

import (
	"fmt"
	"net"
	"strings"
)

var (
	Devices []AirplayDevice
)

type AirplayDevice struct {
	Name     string
	Hostname string
	IP       net.IP
	Port     uint16
	Flags    []string
}

//
// Main functions for starting up and listening for records start here
//

func main() {
	fmt.Println("Starting up...")
	// Listen on the multicast address and port
	socket, err := net.ListenMulticastUDP("udp", nil, &net.UDPAddr{
		IP:   net.IPv4(224, 0, 0, 251),
		Port: 5353,
	})
	if err != nil {
		panic(err)
	}
	// Don't forget to close it!
	defer socket.Close()

	// Put the listener in its own goroutine
	fmt.Println("Waiting for messages...")
	msgs := make(chan DNSMessage)
	go listen(socket, msgs)

	// Bootstrap us by sending a query for any airplay-related entries
	var msg DNSMessage

	q := Question{
		Name:  "_raop._tcp.local.",
		Type:  12, // PTR
		Class: 1,
	}
	msg.AddQuestion(q)

	q = Question{
		Name:  "_airplay._tcp.local.",
		Type:  12, // PTR
		Class: 1,
	}
	msg.AddQuestion(q)

	buffer, err := msg.Pack()
	if err != nil {
		panic(err)
	}

	// Write the payload
	_, err = socket.WriteToUDP(buffer, &net.UDPAddr{
		IP:   net.IPv4(224, 0, 0, 251),
		Port: 5353,
	})
	if err != nil {
		panic(err)
	}

	// Wait for a message from the listen goroutine
	fmt.Println("Ctrl+C to exit")
	for {
		msg = <-msgs

		// Look for new devices
		for i := range msg.Answers {
			rr := &msg.Answers[i]

			// PTRs only
			if rr.Type != 12 {
				continue
			}

			// Figure out the name of this thing
			nameParts := strings.Split(rr.Rdata.(PTRRecord).Name, ".")
			deviceName := nameParts[0]

			// If this is a device we already know about, then ignore it
			// Otherwise, add it
			index := -1
			for i := range Devices {
				if Devices[i].Name == deviceName {
					index = i
					break
				}
			}

			if index == -1 {
				Devices = append(Devices, AirplayDevice{
					Name: deviceName,
				})

				index = len(Devices) - 1
			}
		}

		// See if the rest of the information for this device is in Extras
		for i := range msg.Extras {
			rr := &msg.Extras[i]

			// Figure out the name of this thing
			nameParts := strings.Split(rr.Name, ".")
			deviceName := nameParts[0]

			// Find our existing device
			for j := range Devices {
				if Devices[j].Name == deviceName || Devices[j].Hostname == rr.Name {
					// Found it, now update it
					switch rr.Type {
					case 1: // A
						Devices[j].IP = rr.Rdata.(ARecord).Address
						break

					case 16: // TXT
						Devices[j].Flags = rr.Rdata.(TXTRecord).CStrings
						break

					case 33: // SRV
						srv := rr.Rdata.(SRVRecord)
						Devices[j].Hostname = srv.Target
						Devices[j].Port = srv.Port
						break
					}
					break
				}
			}
		}

		// Ask for info on devices we don't have all the information about
		// TODO: Do this on a timer so we're not asking for things too often

		fmt.Printf("%+v\n", Devices)
	}
}

// Listen on a socket for multicast records and parse them
func listen(socket *net.UDPConn, msgs chan DNSMessage) {
	var msg DNSMessage
	// Loop forever waiting for messages
	for {
		// Buffer for the message
		buffer := make([]byte, 4096)
		// Block and wait for a message on the socket
		read, _, err := socket.ReadFromUDP(buffer)
		if err != nil {
			panic(err)
		}

		// Parse the buffer (up to "read" bytes) into a message object
		err = msg.Parse(buffer[:read])
		if err != nil {
			panic(err)
		}

		// Print out the source address and the message
		//fmt.Printf("\nBroadcast from %s:\n%s\n", addr, msg.String())

		// Does this have answers we are interested in? If so, return the whole messaage since the rest of it (Extras in particular)
		// is probably relevant
		for i := range msg.Answers {
			rr := &msg.Answers[i]

			// PTRs only
			if rr.Type != 12 {
				continue
			}

			// Is this an airplay address
			nameParts := strings.Split(rr.Name, ".")
			if nameParts[0] == "_raop" || nameParts[1] == "_airplay" {
				//fmt.Println(msg.String())
				msgs <- msg
			}
		}
	}
}
