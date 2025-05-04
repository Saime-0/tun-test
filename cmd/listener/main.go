package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os/exec"
	"runtime"

	"github.com/songgao/water"

	"tun/internal/consts"
)

func main() {
	if runtime.GOOS != "linux" {
		log.Fatalf("%s is not supported on this platform", runtime.GOOS)
	}

	if err := runListener(); err != nil {
		log.Fatalf("runListener: %v", err)
	}
}

// runListener Поднимает tun интерфейс и отправляет туда трафик, сами пакеты из тоннеля считывается, парсится протокол,
// от кого и кому, по каким портам, логируется полученная информация и байты самого пакета.
//
// Функция в себе содержит:
//  1. Создание tun интерфейса.
//  2. Генерация трафика на девайсе.
//  3. Чтение трафика:
//     3.1. Получить информацию из пакета: Используемый протокол; Какие порты используются.
//     3.2. Залогировать полученную информацию и содержимое пакета (байты).
func runListener() (err error) {
	var ifce *water.Interface
	// Инициализировать интерфейс
	if ifce, err = initTunIfce(); err != nil {
		return fmt.Errorf("initTunIfce: %v", err)
	}
	// Настроить интерфейс
	if err = setupIfce(ifce); err != nil {
		return fmt.Errorf("setupIfce: %v", err)
	}

	// Запустить чтение трафика
	go readPacketsIPv4(ifce)
	//go listenUDP()

	return <-make(chan error)
}

func listenUDP() {
	laddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", consts.IfceIP, consts.UDPPort))
	if err != nil {
		log.Fatalf("listenUDP: net.ResolveUDPAddr: %v", err)
	}
	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		log.Fatalf("listenUDP: net.ListenUDP: %v", err)
	}
	for {
		buf := make([]byte, consts.MTU)
		if n, raddr, err := conn.ReadFromUDP(buf); err != nil {
			log.Fatalf("ERROR listenUDP: conn.Read: %v", err)
		} else {
			log.Printf("INFO listenUDP: data=%#v raddr=%#v", string(buf[:n]), raddr)
		}
	}
}

// initTunIfce создает виртуальные сетевые интерфейс типа TUN
func initTunIfce() (*water.Interface, error) {
	config := water.Config{
		DeviceType: water.TUN,
		PlatformSpecificParams: water.PlatformSpecificParams{
			Name: consts.IfceName,
		},
	}

	ifce, err := water.New(config)
	if err != nil {
		return nil, fmt.Errorf("water.New: %w", err)
	}

	return ifce, nil
}

// setupIfce настраивает интерфейс посредством утилиты ip
func setupIfce(ifce *water.Interface) error {
	// Установить размер одного пакета
	if err := exec.Command("/sbin/ip", "link", "set", "dev", ifce.Name(), "mtu", fmt.Sprint(consts.MTU)).Run(); err != nil {
		return fmt.Errorf("exec.Command: %v", err)
	}
	// Назначить адрес интерфейсу ipv4 адрес
	if err := exec.Command("/sbin/ip", "addr", "add", consts.IfceCIDR, "dev", ifce.Name()).Run(); err != nil {
		return fmt.Errorf("exec.Command: %v", err)
	}
	// Включить интерфейс
	if err := exec.Command("/sbin/ip", "link", "set", "dev", ifce.Name(), "up").Run(); err != nil {
		return fmt.Errorf("exec.Command: %v", err)
	}
	// Удалить ipv6 (чтобы не мешал)
	if err := exec.Command("/sbin/ip", "-6", "addr", "flush", "dev", consts.IfceName).Run(); err != nil {
		return fmt.Errorf("exec.Command: %v", err)
	}

	return nil
}

// readPacketsIPv4 читает трафик
func readPacketsIPv4(ifce *water.Interface) {
	//var msg []byte
	buf := make([]byte, consts.MTU)
	for {
		n, err := ifce.Read(buf)
		if err != nil {
			log.Printf("ERROR readPacketsIPv4: %v", err)
			continue
		}
		//msg = append(msg, buf[:n]...)
		//if n == MTU {
		//	continue
		//}
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
		protocol: protoName(b[9]),
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
