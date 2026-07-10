package mc

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// PingResult is what the Minecraft server list ping reports.
type PingResult struct {
	Online int `json:"online"`
	Max    int `json:"max"`
}

// PingStatus performs a modern (1.7+) server list ping against localhost.
// It needs no authentication and works regardless of online-mode, but the
// server must have enable-status=true (the default).
func PingStatus(port int, timeout time.Duration) (*PingResult, error) {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), timeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(timeout))

	// Handshake: id 0, protocol -1 (status), address, port, next state 1.
	var hs bytes.Buffer
	writeVarInt(&hs, 0)
	writeVarInt(&hs, -1)
	writeVarInt(&hs, int32(len("127.0.0.1")))
	hs.WriteString("127.0.0.1")
	binary.Write(&hs, binary.BigEndian, uint16(port))
	writeVarInt(&hs, 1)
	if err := writePacket(conn, hs.Bytes()); err != nil {
		return nil, err
	}
	// Status request: empty packet with id 0.
	if err := writePacket(conn, []byte{0x00}); err != nil {
		return nil, err
	}

	br := bufio.NewReader(conn)
	if _, err := readVarInt(br); err != nil { // total packet length
		return nil, err
	}
	id, err := readVarInt(br)
	if err != nil {
		return nil, err
	}
	if id != 0 {
		return nil, fmt.Errorf("unexpected packet id %d", id)
	}
	strLen, err := readVarInt(br)
	if err != nil {
		return nil, err
	}
	if strLen < 0 || strLen > 1<<21 {
		return nil, errors.New("status response too large")
	}
	payload := make([]byte, strLen)
	if _, err := io.ReadFull(br, payload); err != nil {
		return nil, err
	}
	var status struct {
		Players struct {
			Online int `json:"online"`
			Max    int `json:"max"`
		} `json:"players"`
	}
	if err := json.Unmarshal(payload, &status); err != nil {
		return nil, err
	}
	return &PingResult{Online: status.Players.Online, Max: status.Players.Max}, nil
}

// PingBedrock performs a RakNet unconnected ping against a local Bedrock
// server (UDP). The pong carries player counts in a semicolon-separated string.
func PingBedrock(port int, timeout time.Duration) (*PingResult, error) {
	conn, err := net.DialTimeout("udp", fmt.Sprintf("127.0.0.1:%d", port), timeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(timeout))

	magic := []byte{0x00, 0xff, 0xff, 0x00, 0xfe, 0xfe, 0xfe, 0xfe, 0xfd, 0xfd, 0xfd, 0xfd, 0x12, 0x34, 0x56, 0x78}
	var req bytes.Buffer
	req.WriteByte(0x01) // unconnected ping
	binary.Write(&req, binary.BigEndian, time.Now().UnixMilli())
	req.Write(magic)
	binary.Write(&req, binary.BigEndian, uint64(0x1234567890abcdef)) // client guid
	if _, err := conn.Write(req.Bytes()); err != nil {
		return nil, err
	}

	resp := make([]byte, 2048)
	n, err := conn.Read(resp)
	if err != nil {
		return nil, err
	}
	// 0x1c pong: id(1) time(8) guid(8) magic(16) strlen(2) payload
	if n < 36 || resp[0] != 0x1c {
		return nil, errors.New("unexpected bedrock pong")
	}
	fields := strings.Split(string(resp[35:n]), ";")
	// MCPE;motd;protocol;version;online;max;...
	if len(fields) < 6 {
		return nil, errors.New("short bedrock pong")
	}
	online, err1 := strconv.Atoi(fields[4])
	max, err2 := strconv.Atoi(fields[5])
	if err1 != nil || err2 != nil {
		return nil, errors.New("unparsable bedrock pong")
	}
	return &PingResult{Online: online, Max: max}, nil
}

func writePacket(w io.Writer, payload []byte) error {
	var buf bytes.Buffer
	writeVarInt(&buf, int32(len(payload)))
	buf.Write(payload)
	_, err := w.Write(buf.Bytes())
	return err
}

func writeVarInt(buf *bytes.Buffer, v int32) {
	u := uint32(v)
	for {
		b := byte(u & 0x7f)
		u >>= 7
		if u != 0 {
			b |= 0x80
		}
		buf.WriteByte(b)
		if u == 0 {
			return
		}
	}
}

func readVarInt(r io.ByteReader) (int32, error) {
	var result uint32
	for i := 0; i < 5; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		result |= uint32(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return int32(result), nil
		}
	}
	return 0, errors.New("varint too long")
}
