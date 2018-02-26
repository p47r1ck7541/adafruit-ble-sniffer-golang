package sniffer

import "io"
import (
	"github.com/yinghau76/adafruit-ble-sniffer-golang/slip"
	"github.com/jacobsa/go-serial/serial"
	"log"
	"bytes"
	"time"
	"context"
)

const (
	REQ_FOLLOW              = 0x00
	RESP_FOLLOW             = 0x01
	EVENT_DEVICE            = 0x02
	REQ_SINGLE_PACKET       = 0x03
	RESP_SINGLE_PACKET      = 0x04
	EVENT_CONNECT           = 0x05
	EVENT_PACKET            = 0x06
	REQ_SCAN_CONT           = 0x07
	RESP_SCAN_CONT          = 0x08
	EVENT_DISCONNECT        = 0x09
	EVENT_ERROR             = 0x0A
	EVENT_EMPTY_DATA_PACKET = 0x0B
	SET_TEMPORARY_KEY       = 0x0C
	PING_REQ                = 0x0D
	PING_RESP               = 0x0E
	TEST_COMMAND_ID         = 0x0F
	TEST_RESULT_ID          = 0x10
	UART_TEST_START         = 0x11
	UART_DUMMY_PACKET       = 0x12
	SWITCH_BAUD_RATE_REQ    = 0x13
	SWITCH_BAUD_RATE_RESP   = 0x14
	UART_OUT_START          = 0x15
	UART_OUT_STOP           = 0x16
	SET_ADV_CHANNEL_HOP_SEQ = 0x17
	GO_IDLE                 = 0xFE
)

type Sniffer struct {
	port               io.ReadWriteCloser
	commandPacketCount int

	wr *slip.SlipWriter
	rd *slip.SlipReader

	buf     []byte
	devices []*Device
}

func NewSniffer() *Sniffer {
	options := serial.OpenOptions{
		PortName:          "/dev/cu.SLAB_USBtoUART",
		BaudRate:          460800,
		DataBits:          8,
		StopBits:          1,
		RTSCTSFlowControl: true,
		InterCharacterTimeout:   1000,
	}
	port, err := serial.Open(options)
	if err != nil {
		log.Fatalf("serial.Open: %v", err)
	}

	return &Sniffer{
		port:               port,
		commandPacketCount: 0,
		rd:                 slip.NewSlipReader(port),
		wr:                 slip.NewSlipWriter(port),
		buf:                make([]byte, 1024),
	}
}

func (s *Sniffer) Close() {
	s.port.Close()
}

func (s *Sniffer) Ping(ctx context.Context) (*PingResponse, error) {
	s.sendCommand(PING_REQ, nil)
	rsp, err := s.waitForPacket(ctx, PING_RESP)
	if err != nil {
		return nil, err
	}
	return rsp.PingResponse, nil
}

func (s *Sniffer) Scan(ctx context.Context) (*ScanResponse, error) {
	s.sendCommand(REQ_SCAN_CONT, nil)
	if false { // TODO: it seems that the firmware won't send scan response
		rsp, err := s.waitForPacket(ctx, RESP_SCAN_CONT)
		if err != nil {
			return nil, err
		}
		return rsp.ScanResponse, nil
	}
	return nil, nil
}

func (s *Sniffer) sendCommand(cmd int, payload []byte) {
	packet := make([]byte, 0, 32)
	packet = append(packet, 6) // header length
	packet = append(packet, byte(len(payload)))
	packet = append(packet, 1)                                                                       // protocol version
	packet = append(packet, byte(s.commandPacketCount&0xff), byte((s.commandPacketCount&0xff00)>>8)) // packet counter
	packet = append(packet, byte(cmd))
	packet = append(packet, payload...)

	if _, err := s.wr.Write(packet); err != nil {
		log.Fatalf("Failed to write: %v", err)
	}
	s.commandPacketCount++
}

func (s *Sniffer) ReadPacket() (*Packet, error) {
	l, err := s.rd.Read(s.buf)
	if err != nil {
		return nil, err
	}
	return parsePacket(s.buf[:l])
}

func (s *Sniffer) ScanDevices(scanDuration time.Duration) ([]*Device, error) {
	ctx, _ := context.WithTimeout(context.Background(), scanDuration)
	if _, err := s.Ping(ctx); err != nil {
		return nil, err
	}
	if _, err := s.Scan(ctx); err != nil {
		return nil, err
	}

	s.clearDevices()
	for {
		select {
		case <-ctx.Done():
			return s.devices, nil
		default:
			event, _ := s.waitForPacket(ctx, EVENT_PACKET)
			if device := NewDevice(event); device != nil {
				s.addOrUpdateDevice(device)
			}
		}
	}
}

func (s *Sniffer) WaitForPacket(packetId int, timeout time.Duration) (*Packet, error) {
	ctx, _ := context.WithTimeout(context.Background(), timeout)
	return s.waitForPacket(ctx, packetId)
}

func (s *Sniffer) waitForPacket(ctx context.Context, packetId int) (*Packet, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			p, err := s.ReadPacket()
			if err == nil && p.Id == byte(packetId) {
				return p, nil
			}
		}
	}
}

func (s *Sniffer) clearDevices() {
	s.devices = []*Device{}
}

func (s *Sniffer) addOrUpdateDevice(device *Device) {
	for i, dev := range s.devices {
		if bytes.Compare(dev.Address, device.Address) == 0 {
			s.devices[i] = device
			return
		}
	}

	s.devices = append(s.devices, device)
}