package main

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"log"
	"net"
	"os/exec"
	"runtime"
	"time"

	"github.com/songgao/water"
)

func main() {
	if runtime.GOOS != "linux" {
		log.Fatalf("%s is not supported on this platform", runtime.GOOS)
	}

	if err := runTunApp(); err != nil {
		log.Fatal("runTunApp: ", err.Error())
	}
}

const (
	MTU      = 1500 // Размер ipv4 пакетов
	ifceName = "tun250413"
	ifceIP   = "10.1.0.10"
	ifceCIDR = ifceIP + "/24"
	udpPort  = 50500
)

// runTunApp Поднимает tun интерфейс и отправляет туда трафик, сами пакеты из тоннеля считывается, парсится протокол,
// от кого и кому, по каким портам, логируется полученная информация и байты самого пакета.
//
// Функция в себе содержит:
//  1. Создание tun интерфейса.
//  2. Генерация трафика на девайсе.
//  3. Чтение трафика:
//     3.1. Получить информацию из пакета: Используемый протокол; Какие порты используются.
//     3.2. Залогировать полученную информацию и содержимое пакета (байты).
func runTunApp() (err error) {
	var ifce *water.Interface
	// Инициализировать интерфейс
	if ifce, err = initTunIfce(); err != nil {
		return err
	}
	// Настроить интерфейс
	if err = setupIfce(ifce); err != nil {
		return fmt.Errorf("setupIfce: %v", err)
	}

	// Запустить чтение трафика
	go readPacketsIPv4(ifce)
	//go listenUDP()
	// Запустить запись трафика
	go writePacketsUDP4()

	return <-make(chan error)
}

func listenUDP() {
	laddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", ifceIP, udpPort))
	if err != nil {
		log.Fatalf("listenUDP: net.ResolveUDPAddr: %v", err)
	}
	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		log.Fatalf("listenUDP: net.ListenUDP: %v", err)
	}
	for {
		buf := make([]byte, MTU)
		if n, raddr, err := conn.ReadFromUDP(buf); err != nil {
			log.Fatalf("ERROR listenUDP: conn.Read: %v", err)
		} else {
			log.Printf("INFO listenUDP: data=%#v raddr=%#v", string(buf[:n]), raddr)
		}
	}
}

func writePacketsUDP4() {
	conn, err := net.DialUDP(
		"udp",
		&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 51442},
		//nil,
		&net.UDPAddr{IP: net.ParseIP(ifceIP), Port: udpPort},
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
			DstPort:  udpPort,
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
		length int = len(data)
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
	sum += (sum >> 16)

	return uint16(^sum)
}

// initTunIfce создает виртуальные сетевые интерфейс типа TUN
func initTunIfce() (*water.Interface, error) {
	config := water.Config{
		DeviceType: water.TUN,
	}
	config.Name = ifceName

	ifce, err := water.New(config)
	if err != nil {
		log.Fatal(err)
	}

	return ifce, nil
}

// setupIfce настраивает интерфейс посредством утилиты ip
func setupIfce(ifce *water.Interface) error {
	// Установить размер одного пакета
	if err := exec.Command("/sbin/ip", "link", "set", "dev", ifce.Name(), "mtu", fmt.Sprint(MTU)).Run(); err != nil {
		return fmt.Errorf("exec.Command: %v", err)
	}
	// Назначить адрес интерфейсу ipv4 адрес
	if err := exec.Command("/sbin/ip", "addr", "add", ifceCIDR, "dev", ifce.Name()).Run(); err != nil {
		return fmt.Errorf("exec.Command: %v", err)
	}
	// Включить интерфейс
	if err := exec.Command("/sbin/ip", "link", "set", "dev", ifce.Name(), "up").Run(); err != nil {
		return fmt.Errorf("exec.Command: %v", err)
	}
	// Удалить ipv6 (чтобы не мешал)
	if err := exec.Command("/sbin/ip", "-6", "addr", "flush", "dev", ifceName).Run(); err != nil {
		return fmt.Errorf("exec.Command: %v", err)
	}

	return nil
}

// readPacketsIPv4 читает трафик
func readPacketsIPv4(ifce *water.Interface) {
	buf := make([]byte, MTU)
	for {
		n, err := ifce.Read(buf)
		if err != nil {
			log.Printf("ERROR readPacketsIPv4: %v", err)
			continue
		}
		packet := ip4PacketFromBytes(buf[:n])
		log.Printf("INFO readPacketsIPv4: packetIPv4=%v", packet)
		tlp := tlpFromBytes(packet.payload, packet.protocol)
		log.Printf("INFO readPacketsIPv4: tlp=%v", tlp)
	}
}

// packetIPv4 содержит некоторые данные пакета
type packetIPv4 struct {
	protocol string
	payload  []byte
}

func ip4PacketFromBytes(b []byte) packetIPv4 {
	packet := packetIPv4{
		protocol: tlProtocolName[b[9]],
	}
	if len(b) >= 25 {
		packet.payload = b[24:]
	}

	return packet
}

// tlProtocol содержит некоторые данные протокола транспортного уровня
type tlProtocol struct {
	name            string
	sourcePort      int
	destinationPort int
	payload         []byte
}

func tlpFromBytes(b []byte, name string) tlProtocol {
	protocol := tlProtocol{
		name:            name,
		sourcePort:      int(binary.BigEndian.Uint16(b[0:2])),
		destinationPort: int(binary.BigEndian.Uint16(b[2:4])),
	}
	switch protocol.name {
	case "udp":
		if len(b) >= 9 {
			protocol.payload = b[8:]
		}
	case "tcp":
		if len(b) >= 193 {
			protocol.payload = b[192:]
		}
	}

	return protocol
}

// tlProtocolName человеко-понятные названия протоколов транспортного уровня
var tlProtocolName = map[byte]string{
	0x00: "HOPOPT",
	0x01: "ICMP",
	0x02: "IGMP",
	0x03: "GGP",
	0x04: "IPv4Encapsulation",
	0x05: "ST",
	0x06: "TCP",
	0x07: "CBT",
	0x08: "EGP",
	0x09: "IGP",
	0x0A: "BBN_RCC_MON",
	0x0B: "NVP_II",
	0x0C: "PUP",
	0x0D: "ARGUS",
	0x0E: "EMCON",
	0x0F: "XNET",
	0x10: "CHAOS",
	0x11: "UDP",
	0x12: "MUX",
	0x13: "DCN_MEAS",
	0x14: "HMP",
	0x15: "PRM",
	0x16: "XNS_IDP",
	0x17: "TRUNK_1",
	0x18: "TRUNK_2",
	0x19: "LEAF_1",
	0x1A: "LEAF_2",
	0x1B: "RDP",
	0x1C: "IRTP",
	0x1D: "ISO_TP4",
	0x1E: "NETBLT",
	0x1F: "MFE_NSP",
	0x20: "MERIT_INP",
	0x21: "DCCP",
	0x22: "ThirdPC",
	0x23: "IDPR",
	0x24: "XTP",
	0x25: "DDP",
	0x26: "IDPR_CMTP",
	0x27: "TPxx",
	0x28: "IL",
	0x29: "IPv6Encapsulation",
	0x2A: "SDRP",
	0x2B: "IPv6_Route",
	0x2C: "IPv6_Frag",
	0x2D: "IDRP",
	0x2E: "RSVP",
	0x2F: "GRE",
	0x30: "MHRP",
	0x31: "BNA",
	0x32: "ESP",
	0x33: "AH",
	0x34: "I_NLSP",
	0x35: "SWIPE",
	0x36: "NARP",
	0x37: "MOBILE",
	0x38: "TLSP",
	0x39: "SKIP",
	0x3A: "IPv6_ICMP",
	0x3B: "IPv6_NoNxt",
	0x3C: "IPv6_Opts",
	0x3E: "CFTP",
	0x40: "SAT_EXPAK",
	0x41: "KRYPTOLAN",
	0x42: "RVD",
	0x43: "IPPC",
	0x45: "SAT_MON",
	0x46: "VISA",
	0x47: "IPCV",
	0x48: "CPNX",
	0x49: "CPHB",
	0x4A: "WSN",
	0x4B: "PVP",
	0x4C: "BR_SAT_MON",
	0x4D: "SUN_ND",
	0x4E: "WB_MON",
	0x4F: "WB_EXPAK",
	0x50: "ISO_IP",
	0x51: "VMTP",
	0x52: "SECURE_VMTP",
	0x53: "VINES",
	//0x54: "TTP", // obsoleted March 2023
	0x54: "IPTM",
	0x55: "NSFNET_IGP",
	0x56: "DGP",
	0x57: "TCF",
	0x58: "EIGRP",
	0x59: "OSPF",
	0x5A: "Sprite_RPC",
	0x5B: "LARP",
	0x5C: "MTP",
	0x5D: "AX_25",
	0x5E: "IPIP",
	0x5F: "MICP",
	0x60: "SCC_SP",
	0x61: "ETHERIP",
	0x62: "ENCAP",
	0x64: "GMTP",
	0x65: "IFMP",
	0x66: "PNNI",
	0x67: "PIM",
	0x68: "ARIS",
	0x69: "SCPS",
	0x6A: "QNX",
	0x6B: "A_N",
	0x6C: "IPComp",
	0x6D: "SNP",
	0x6E: "Compaq_Peer",
	0x6F: "IPX_in_IP",
	0x70: "VRRP",
	0x71: "PGM",
	0x73: "L2TP",
	0x74: "DDX",
	0x75: "IATP",
	0x76: "STP",
	0x77: "SRP",
	0x78: "UTI",
	0x79: "SMP",
	0x7A: "SM",
	0x7B: "PTP",
	0x7D: "FIRE",
	0x7E: "CRTP",
	0x7F: "CRUDP",
	0x80: "SSCOPMCE",
	0x81: "IPLT",
	0x82: "SPS",
	0x83: "PIPE",
	0x84: "SCTP",
	0x85: "FC",
	0x8A: "manet",
	0x8B: "HIP",
	0x8C: "Shim6",
}

// ======= Модель TCP/IP =======
//
// Номер | Название уровня                | Протоколы
//   4   | Application layer (Прикладной) | HTTP, SSH, Telnet, ...
//   3*  | Transport layer (Транспортный) | TCP, UDP	                    <---- tun находится здесь
//   2   | Internet layer (Межсетевой)    | IP (IPv4, IPv6), ICMP, IGMP
//   1   | Link layer (Канальный)         | Ethernet, Wi-Fi, PPP, DSL
//
// ==== Транспортный уровень ====
//
// IPv4 data unit (packet):                                                              https://pkg.go.dev/github.com/songgao/water/waterutil
// +---------------------------------------------------------------------------------------------------------------+
// |       | Octet |           0           |           1           |           2           |           3           |
// | Octet |  Bit  |00|01|02|03|04|05|06|07|08|09|10|11|12|13|14|15|16|17|18|19|20|21|22|23|24|25|26|27|28|29|30|31|
// +---------------------------------------------------------------------------------------------------------------+
// |   0   |   0   |  Version  |    IHL    |      DSCP       | ECN |                 Total  Length                 |
// +---------------------------------------------------------------------------------------------------------------+
// |   4   |  32   |                Identification                 | Flags  |           Fragment Offset            |
// +---------------------------------------------------------------------------------------------------------------+
// |   8   |  64   |     Time To Live      |       protocol        |                Header Checksum                | <----- Нам требуется залогировать protocol транспортного уровня
// +---------------------------------------------------------------------------------------------------------------+
// |  12   |  96   |                                       Source IP Address                                       |
// +---------------------------------------------------------------------------------------------------------------+
// |  16   |  128  |                                    Destination IP Address                                     |
// +---------------------------------------------------------------------------------------------------------------+
// |  20   |  160  |                                     Options (if IHL > 5)                                      |
// +---------------------------------------------------------------------------------------------------------------+
// |  24   |  192  |                                                                                               |
// |  30   |  224  |                                            payload                                            | <----- Здесь лежит PDU(protocol data unit), который надо декапсулировать
// |  ...  |  ...  | <--- максимум 1500 октетов (байт)                                                             |
// +---------------------------------------------------------------------------------------------------------------+
//
// UDP data unit (datagram):
// +---------------------------------------------------------------------------------------------------------------+
// |       | Octet |           0           |           1           |           2           |           3           |
// | Octet |  Bit  |00|01|02|03|04|05|06|07|08|09|10|11|12|13|14|15|16|17|18|19|20|21|22|23|24|25|26|27|28|29|30|31|
// +---------------------------------------------------------------------------------------------------------------+
// |   0   |   0   |                   source port                 |                 destination port              | <----- Нам требуется залогировать порты
// +---------------------------------------------------------------------------------------------------------------+
// |   4   |  32   |                     Length                    |                     checksum                  |
// +---------------------------------------------------------------------------------------------------------------+
// |   8   |  64   |                                                                                               |
// |  12   |  96   |                                            payload                                            |
// |  ...  |  ...  |                                                                                               |
// +---------------------------------------------------------------------------------------------------------------+
//

// Источники:
// Принцип VPN, используя water:
//	https://github.com/roundliuyang/Computer-Network/blob/3d581ce3dbf41a9ba87cfa82cb510599310c5290/Linux%20%E5%86%85%E6%A0%B8%E7%BD%91%E7%BB%9C%E6%8A%80%E6%9C%AF/VPN%20%E5%8E%9F%E7%90%86.md?plain=1#L90
// A simple TUN/TAP library written in native Go:
//	https://github.com/songgao/water
// Модель OSI | Введение в сети. Часть 2:
// 	https://youtu.be/YX3lWjNu58k?si=11U-PfH9GjLydtS1
// Модель TCP/IP | Введение в сети. Часть 3:
// 	https://youtu.be/XGiezoHclP8?si=v89iVMdgXjZa0xne
// КАК УСТРОЕН TCP/IP?:
// 	https://youtu.be/EJzitviiv2c?si=BgFavllcnYG8Fla8
// Software Networking and Interfaces on Linux: Part 1:
// 	https://www.youtube.com/watch?v=EnAZB8GI97c
