package main

import (
	"fmt"
	"log"
	"log/slog"
	"net"
	"os/exec"
	"runtime"
	"time"

	"github.com/google/uuid"
	"github.com/songgao/packets/ethernet"
	"github.com/songgao/water"
	"github.com/songgao/water/waterutil"
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
	MTU        = 1500
	ifceName   = "tun250413"
	ifceIP     = "10.1.0.10"
	ifceIPMask = "24"
	ifceCIDR   = ifceIP + "/" + ifceIPMask
)

// runTunApp Поднимает tun интерфейс и отправляет туда трафик, сами пакеты из тоннеля считывается, парсится протокол,
// от кого и кому, по каким портам, логируется полученная информация и байты самого пакета.
//
// Будет выполнено:
//  1. Поднять tun интерфейс.
//  2. Направлять трафик в интерфейс.
//  3. Считывать пакеты из интерфейса:
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
	// Инициализировать подключение
	//conn, err := initConn()
	//if err != nil {
	//	return fmt.Errorf("initConn: %v", err)
	//}
	// Начать вычитывать трафик
	go readFrames(ifce)
	// Начать писать трафик
	//write(conn)
	writeFrames(ifce)

	return nil
}

func writeFrames(ifce *water.Interface) {
	for range 10 {
		time.Sleep(1 * time.Second)
		b := []byte(uuid.NewString())
		n, err := ifce.Write(b)
		if err != nil {
			log.Printf("ERROR writeFrames: ifce.Write: %v", err)
		}
		log.Printf("INFO writeFrames: ifce.Write: written %d bytes: % x", n, b)
	}
}

func write(conn net.Conn) {
	for range 10 {
		time.Sleep(1 * time.Second)
		data := uuid.NewString()
		n, err := fmt.Fprintf(conn, data)
		if err != nil {
			log.Printf("ERROR write: fmt.Fprintf: %v", err)
			continue
		}
		log.Printf("INFO wrote frame: %d bytes: % x", n, data)
	}
}

//func write(conn net.Conn) {
//	for range 10 {
//		time.Sleep(1 * time.Second)
//		b := []byte(uuid.NewString())
//		n, err := conn.Write(b)
//		if err != nil {
//			log.Printf("ERROR write: conn.Write: %v", err)
//		}
//		log.Printf("INFO wrote UDP frames: %d bytes: % x", n, b)
//	}
//}

func initConn() (net.Conn, error) {
	conn, err := net.Dial("udp", ifceIP+":42224")
	if err != nil {
		return nil, fmt.Errorf("net.Dial: %v", err)
	}

	return conn, err
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

// readFrames читает пачками
func readFrames(ifce *water.Interface) {
	var frame ethernet.Frame
	for {
		frame.Resize(MTU)
		n, err := ifce.Read(frame)
		if err != nil {
			log.Printf("ERROR readFrames: %v", err)
			continue
		}
		frame = frame[:n]
		logNewFrame(frame)
	}
}

// logNewFrame логирует некоторые данные фрейма
func logNewFrame(frame ethernet.Frame) {
	p := waterutil.IPv4Protocol(frame)
	//log.Printf("Протокол: %x (%s)\n", p, protocolName[p])
	//log.Printf("Порт назначения: %d\n", waterutil.IPv4DestinationPort(frame))
	//log.Printf("Порт источника: %d\n", waterutil.IPv4SourcePort(frame))
	//log.Printf("Данные: % x\n", frame.Payload())
	slog.Info(
		"logNewFrame",
		slog.String("Протокол", protocolName[p]),
		slog.Int("Порт назначения", int(waterutil.IPv4DestinationPort(frame))),
		slog.Int("Порт источника", int(waterutil.IPv4SourcePort(frame))),
		slog.String("Данные", fmt.Sprintf("% x", frame.Payload())),
	)
}

// readFrames человеко-понятные названия протоколов
var protocolName = map[waterutil.IPProtocol]string{
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

// Источники:
// Принцип VPN, используя water:
//	https://github.com/roundliuyang/Computer-Network/blob/3d581ce3dbf41a9ba87cfa82cb510599310c5290/Linux%20%E5%86%85%E6%A0%B8%E7%BD%91%E7%BB%9C%E6%8A%80%E6%9C%AF/VPN%20%E5%8E%9F%E7%90%86.md?plain=1#L90
// A simple TUN/TAP library written in native Go:
//	https://github.com/songgao/water
//
