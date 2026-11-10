package proxy

import (
	"errors"
	"net"
	"strconv"
	"strings"

	"red-token/internal/domain"
)

func ValidateServer(server domain.ProxyServer) error {
	switch domain.NormalizeProxyProtocol(server.Protocol) {
	case domain.ProxyProtocolSOCKS5:
	case domain.ProxyProtocolTransparent:
		if server.Username != "" || server.Password != "" {
			return errors.New("transparent TCP proxy does not support username/password authentication")
		}
	default:
		return errors.New("proxy protocol must be socks5 or transparent")
	}

	address := strings.TrimSpace(server.Address)
	if address == "" {
		return errors.New("proxy address is required")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return errors.New("proxy address must be host:port")
	}
	if strings.TrimSpace(host) == "" {
		return errors.New("proxy address must include host")
	}
	parsedPort, err := strconv.Atoi(port)
	if err != nil || parsedPort < 1 || parsedPort > 65535 {
		return errors.New("proxy address port must be 1-65535")
	}
	return nil
}
