package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

// Minimal Minecraft RCON client (Source RCON protocol).

const (
	rconAuth        = 3
	rconExecCommand = 2
)

type RconClient struct {
	conn  net.Conn
	reqID int32
}

func RconDial(addr, password string) (*RconClient, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	c := &RconClient{conn: conn}
	resp, id, err := c.send(rconAuth, password)
	if err != nil {
		conn.Close()
		return nil, err
	}
	_ = resp
	if id == -1 {
		conn.Close()
		return nil, fmt.Errorf("rcon auth failed")
	}
	return c, nil
}

func (c *RconClient) Close() error { return c.conn.Close() }

func (c *RconClient) Command(cmd string) (string, error) {
	resp, _, err := c.send(rconExecCommand, cmd)
	return resp, err
}

func (c *RconClient) send(typ int32, payload string) (string, int32, error) {
	c.reqID++
	id := c.reqID

	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, int32(len(payload)+10))
	binary.Write(&buf, binary.LittleEndian, id)
	binary.Write(&buf, binary.LittleEndian, typ)
	buf.WriteString(payload)
	buf.Write([]byte{0, 0})

	c.conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := c.conn.Write(buf.Bytes()); err != nil {
		return "", 0, err
	}

	var length, respID, respType int32
	if err := binary.Read(c.conn, binary.LittleEndian, &length); err != nil {
		return "", 0, err
	}
	if err := binary.Read(c.conn, binary.LittleEndian, &respID); err != nil {
		return "", 0, err
	}
	if err := binary.Read(c.conn, binary.LittleEndian, &respType); err != nil {
		return "", 0, err
	}
	body := make([]byte, length-8)
	read := 0
	for read < len(body) {
		n, err := c.conn.Read(body[read:])
		if err != nil {
			return "", 0, err
		}
		read += n
	}
	// Strip the two trailing NULs.
	resp := string(bytes.TrimRight(body, "\x00"))
	return resp, respID, nil
}
