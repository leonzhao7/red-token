package proxy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

type socks5Dialer struct {
	address  string
	username string
	password string
}

func (d *socks5Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("unsupported socks5 network: %s", network)
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", strings.TrimSpace(d.address))
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	if err := d.handshake(conn, address); err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func (d *socks5Dialer) handshake(conn net.Conn, target string) error {
	methods := []byte{0x00}
	if d.username != "" || d.password != "" {
		methods = append(methods, 0x02)
	}
	if _, err := conn.Write([]byte{0x05, byte(len(methods))}); err != nil {
		return err
	}
	if _, err := conn.Write(methods); err != nil {
		return err
	}

	var methodReply [2]byte
	if _, err := io.ReadFull(conn, methodReply[:]); err != nil {
		return err
	}
	if methodReply[0] != 0x05 {
		return errors.New("invalid socks5 greeting response")
	}
	switch methodReply[1] {
	case 0x00:
	case 0x02:
		if err := d.authenticate(conn); err != nil {
			return err
		}
	default:
		return errors.New("socks5 proxy rejected authentication methods")
	}

	request, err := socks5ConnectRequest(target)
	if err != nil {
		return err
	}
	if _, err := conn.Write(request); err != nil {
		return err
	}

	var header [4]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return err
	}
	if header[0] != 0x05 {
		return errors.New("invalid socks5 connect response")
	}
	if header[1] != 0x00 {
		return fmt.Errorf("socks5 connect failed: %s", socks5ReplyName(header[1]))
	}
	return discardSocks5BindAddress(conn, header[3])
}

func (d *socks5Dialer) authenticate(conn net.Conn) error {
	username := []byte(d.username)
	password := []byte(d.password)
	if len(username) > 255 || len(password) > 255 {
		return errors.New("socks5 username/password must be <= 255 bytes")
	}

	request := []byte{0x01, byte(len(username))}
	request = append(request, username...)
	request = append(request, byte(len(password)))
	request = append(request, password...)
	if _, err := conn.Write(request); err != nil {
		return err
	}

	var response [2]byte
	if _, err := io.ReadFull(conn, response[:]); err != nil {
		return err
	}
	if response[0] != 0x01 || response[1] != 0x00 {
		return errors.New("socks5 username/password authentication failed")
	}
	return nil
}

func socks5ConnectRequest(target string) ([]byte, error) {
	host, portValue, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portValue)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid target port: %s", portValue)
	}

	request := []byte{0x05, 0x01, 0x00}
	if ip := net.ParseIP(host); ip != nil {
		if ipv4 := ip.To4(); ipv4 != nil {
			request = append(request, 0x01)
			request = append(request, ipv4...)
		} else {
			request = append(request, 0x04)
			request = append(request, ip.To16()...)
		}
	} else {
		hostBytes := []byte(host)
		if len(hostBytes) > 255 {
			return nil, errors.New("socks5 target hostname too long")
		}
		request = append(request, 0x03, byte(len(hostBytes)))
		request = append(request, hostBytes...)
	}

	var portBytes [2]byte
	binary.BigEndian.PutUint16(portBytes[:], uint16(port))
	request = append(request, portBytes[:]...)
	return request, nil
}

func discardSocks5BindAddress(conn net.Conn, atyp byte) error {
	var length int
	switch atyp {
	case 0x01:
		length = net.IPv4len
	case 0x04:
		length = net.IPv6len
	case 0x03:
		var size [1]byte
		if _, err := io.ReadFull(conn, size[:]); err != nil {
			return err
		}
		length = int(size[0])
	default:
		return errors.New("invalid socks5 bind address type")
	}

	if length > 0 {
		if _, err := io.CopyN(io.Discard, conn, int64(length)); err != nil {
			return err
		}
	}
	_, err := io.CopyN(io.Discard, conn, 2)
	return err
}

func socks5ReplyName(code byte) string {
	switch code {
	case 0x01:
		return "general failure"
	case 0x02:
		return "connection not allowed"
	case 0x03:
		return "network unreachable"
	case 0x04:
		return "host unreachable"
	case 0x05:
		return "connection refused"
	case 0x06:
		return "ttl expired"
	case 0x07:
		return "command not supported"
	case 0x08:
		return "address type not supported"
	default:
		return fmt.Sprintf("reply code %d", code)
	}
}
