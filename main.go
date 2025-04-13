package main

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"

	"github.com/songgao/packets/ethernet"
	"github.com/songgao/water"
	"github.com/songgao/water/waterutil"
)

func main() {
	// Что такое tun интерфейс?
	// На каком уровне находится протокол, как определить протокол пакета в linux
	// Как в go lang работать с интерфейсами

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
// Требуется:
//  1. поднять tun интерфейс
//  2. Направлять трафик в интерфейс
//  3. Считывать пакеты из тоннеля
//  4. Получить информацию из пакета:
//     4.1. Используемый протокол
//     4.2. Какие порты используются
//
// 5. Залогировать полученную информацию и содержимое пакета (байты)
func runTunApp() (err error) {
	var ifce *water.Interface
	// Инициализировать интерфейс
	if ifce, err = initTunIfce(); err != nil {
		return err
	}
	//// Установить размер одного пакета
	//if err = exec.Command("/sbin/ip", "link", "set", "dev", ifce.Name(), "mtu", fmt.Sprint(MTU)).Run(); err != nil {
	//	return fmt.Errorf("exec.Command: %v", err)
	//}
	// Назначить адрес интерфейсу ipv4 адрес
	if err = exec.Command("/sbin/ip", "addr", "add", ifceCIDR, "dev", ifce.Name()).Run(); err != nil {
		return fmt.Errorf("exec.Command: %v", err)
	}
	//// Запретить ipv6
	//if err = exec.Command("/usr/bin/sysctl", "-w", fmt.Sprintf("net.ipv6.conf.%s.disable_ipv6=0", ifceName)).Run(); err != nil {
	//	return fmt.Errorf("exec.Command: %v", err)
	//}
	// Включить интерфейс
	if err = exec.Command("/sbin/ip", "link", "set", "dev", ifce.Name(), "up").Run(); err != nil {
		return fmt.Errorf("exec.Command: %v", err)
	}
	// Удалить ipv6 (чтобы не мешал)
	if err = exec.Command("/sbin/ip", "-6", "addr", "flush", "dev", ifceName).Run(); err != nil {
		return fmt.Errorf("exec.Command: %v", err)
	}

	// Начать вычитывать траффик
	readFrames(ifce)
	//readFramesV2(ifce)

	return nil
}

// Источники:
// Принцип VPN, используя water:
//	https://github.com/roundliuyang/Computer-Network/blob/3d581ce3dbf41a9ba87cfa82cb510599310c5290/Linux%20%E5%86%85%E6%A0%B8%E7%BD%91%E7%BB%9C%E6%8A%80%E6%9C%AF/VPN%20%E5%8E%9F%E7%90%86.md?plain=1#L90
// A simple TUN/TAP library written in native Go:
//	https://github.com/songgao/water
//

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

func readFrames(ifce *water.Interface) {
	var frame ethernet.Frame
	for {
		frame.Resize(MTU)
		n, err := ifce.Read(frame)
		if err != nil {
			log.Printf("ERROR readFrames: %s", err.Error())
			continue
		}
		frame = frame[:n]
		logFrameV3(frame)
	}
}

func readFramesV2(ifce *water.Interface) {
	//var frame ethernet.Frame
	//for {
	//	frame.Resize(MTU)
	//	n, err := ifce.Read(frame)
	//	if err != nil {
	//		log.Printf("ERROR readFrames: %s", err.Error())
	//		continue
	//	}
	//	frame = frame[:n]
	//	logFrame(frame)
	//}
	buf := make([]byte, MTU)
	for {
		n, err := ifce.Read(buf)
		if err != nil {
			log.Printf("ERROR readFramesV2: %s", err.Error())
		}
		data := buf[:n]
		//if !waterutil.IsIPv4(data) {
		//	continue
		//}
		logFrameV2(data)
		//logFrame(data)

		//// 如果目标IP与本地 Tun 设备的IP相同，则将数据包写回到 Tun 设备
		//if srcIp.Equal(destIp) {
		//	_, _ = dev.Write(data)
		//} else {
		//	// TODO 将数据通过公网发送给服务端进行转发，并将公网响应数据包写入 Tun 设备
		//	fmt.Println("公网转发")
		//}
	}
}

func logFrame(frame ethernet.Frame) {
	log.Printf("Dst: %s\n", frame.Destination())
	log.Printf("Src: %s\n", frame.Source())
	log.Printf("Протокол: % x\n", frame.Ethertype())
	log.Printf("Порт назначения: % x\n", waterutil.IPv4DestinationPort(frame))
	log.Printf("Порт источника: % x\n", waterutil.IPv4SourcePort(frame))
	log.Printf("Данные: % x\n", frame.Payload())
}

func logFrameV2(data []byte) {
	p := waterutil.IPv4Protocol(data)
	log.Printf("V2: Протокол: %x (%s)\n", p, protocolName[p])
	log.Printf("V2: Порт назначения: %d\n", waterutil.IPv4DestinationPort(data))
	log.Printf("V2: Порт источника: %d\n", waterutil.IPv4SourcePort(data))
	log.Printf("V2: Данные: % x\n", ethernet.Frame(data).Payload())
}

func logFrameV3(frame ethernet.Frame) {
	p := waterutil.IPv4Protocol(frame)
	log.Printf("V3: Протокол: %x (%s)\n", p, protocolName[p])
	log.Printf("V3: Порт назначения: %d\n", waterutil.IPv4DestinationPort(frame))
	log.Printf("V3: Порт источника: %d\n", waterutil.IPv4SourcePort(frame))
	log.Printf("V3: Данные: % x\n", frame.Payload())
}

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
