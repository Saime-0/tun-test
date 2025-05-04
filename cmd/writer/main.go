package main

import (
	"bytes"
	"encoding/gob"
	"log"
	"net"
	"time"

	"tun/internal/consts"
)

func main() {
	// Запустить запись трафика
	if err := runWriter(); err != nil {
		log.Fatalf("runWriter: %v", err)
	}
}

func runWriter() error {
	writePacketsUDP4()
	return nil
}

func writePacketsUDP4() {
	conn, err := net.DialUDP(
		"udp",
		&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 51442},
		//nil,
		&net.UDPAddr{IP: net.ParseIP(consts.IfceName), Port: consts.UDPPort},
		//&net.IPAddr{IP: net.ParseIP(ifceIP)},
		//ifceIP,
	)
	if err != nil {
		log.Fatalf("writePacketsUDP4: net.Dial: %v", err)
	}
	var i int
	for {
		//udpPayload := []byte("hi")
		ipPayload := struct {
			SrcPort  uint16
			DstPort  uint16
			Length   uint16
			Checksum uint16
			Payload  string
		}{
			SrcPort:  50401,
			DstPort:  consts.UDPPort,
			Length:   uint16(len("hello")),
			Checksum: CheckSum([]byte("hello")),
			Payload:  "hello",
		}
		buf := new(bytes.Buffer)
		goben := gob.NewEncoder(buf)
		if err = goben.Encode(ipPayload); err != nil {
			log.Printf("ERROR writePacketsUDP4: goben.Encode: %v", err)
			continue
		}
		//if n, err = conn.Write(buf); err != nil {
		//	if err = binary.Write(conn, binary.BigEndian, ipPayload); err != nil {
		if n, err := conn.Write(buf.Bytes()); err != nil {
			log.Printf("ERROR writePacketsUDP4: conn.Write seq=%d: %v", i, err)
		} else {
			log.Printf("INFO writePacketsUDP4: conn.Write seq=%d: written bytes=%d", i, n)
		}
		i++
		time.Sleep(1 * time.Second)
	}
}

func CheckSum(data []byte) uint16 {
	var (
		sum    uint32
		length = len(data)
		index  int
	)
	for length > 1 {
		sum += uint32(data[index])<<8 + uint32(data[index+1])
		index += 2
		length -= 2
	}
	if length > 0 {
		sum += uint32(data[index])
	}
	sum += sum >> 16

	return uint16(^sum)
}
