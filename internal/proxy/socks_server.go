package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/armon/go-socks5"
	toolLog "github.com/fmotalleb/go-tools/log"
	"go.uber.org/zap"

	"github.com/fmotalleb/bifrost/config"
)

// SOCKSServer is a SOCKS5 proxy that binds outbound connections to selected interfaces.
type SOCKSServer struct {
	cfg           config.Config
	selector      *Selector
	ifaceBindings map[string]ifaceBinding
	ipCache       *IPCache
	telemetry     Telemetry
	connID        atomic.Uint64
}

// NewSOCKSServer constructs a SOCKS5 server from config.
func NewSOCKSServer(cfg config.Config, telemetry Telemetry) (*SOCKSServer, error) {
	runtime, err := prepareRuntimeDependencies(cfg, true, telemetry)
	if err != nil {
		return nil, err
	}

	return &SOCKSServer{
		cfg:           runtime.cfg,
		selector:      runtime.selector,
		ifaceBindings: runtime.bindings,
		ipCache:       runtime.cache,
		telemetry:     runtime.telemetry,
	}, nil
}

// Serve starts listening for SOCKS5 clients.
func (s *SOCKSServer) Serve(ctx context.Context) error {
	logger := toolLog.Of(ctx)
	if !s.cfg.Socks.Listen.IsValid() {
		return errors.New("socks.listen must be a valid address:port")
	}

	serverCfg := &socks5.Config{
		Logger: log.New(socks5DebugLogWriter{logger: logger}, "", 0),
		Dial:   s.buildDialer(ctx),
	}

	username := strings.TrimSpace(s.cfg.Socks.Username)
	password := strings.TrimSpace(s.cfg.Socks.Password)
	if username != "" && password != "" {
		credentials := socks5.StaticCredentials{
			username: password,
		}
		serverCfg.Credentials = credentials
		serverCfg.AuthMethods = []socks5.Authenticator{
			socks5.UserPassAuthenticator{Credentials: credentials},
		}
		logger.Info("socks authentication enabled", zap.String("username", username))
	}

	server, err := socks5.New(serverCfg)
	if err != nil {
		return fmt.Errorf("create socks5 server: %w", err)
	}

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp", s.cfg.Socks.Listen.String())
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.cfg.Socks.Listen, err)
	}
	defer listener.Close()

	logger.Info("socks server listening", zap.String("listen", s.cfg.Socks.Listen.String()))

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	err = server.Serve(listener)
	if err == nil || errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
		return nil
	}
	return fmt.Errorf("serve socks listener: %w", err)
}

type monitoredSOCKSTargetConn struct {
	net.Conn
	ifaceName string
	selector  *Selector
	telemetry Telemetry
	released  sync.Once
	txBytes   atomic.Int64
	rxBytes   atomic.Int64
}

func (c *monitoredSOCKSTargetConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 {
		c.txBytes.Add(int64(n))
		c.telemetry.AddTransfer(c.ifaceName, DirectionTX, int64(n))
	}
	return n, err
}

func (c *monitoredSOCKSTargetConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.rxBytes.Add(int64(n))
		c.telemetry.AddTransfer(c.ifaceName, DirectionRX, int64(n))
	}
	return n, err
}

func (c *monitoredSOCKSTargetConn) CloseWrite() error {
	closeWriter, ok := c.Conn.(interface{ CloseWrite() error })
	if !ok {
		return c.Conn.Close()
	}
	return closeWriter.CloseWrite()
}

func (c *monitoredSOCKSTargetConn) Close() error {
	err := c.Conn.Close()
	c.release(true)
	return err
}

func (c *monitoredSOCKSTargetConn) release(success bool) {
	c.released.Do(func() {
		c.selector.Release(c.ifaceName)
		c.telemetry.ObserveConnection(c.ifaceName, success, c.txBytes.Load(), c.rxBytes.Load())
	})
}

func (s *SOCKSServer) selectDialRoute(addr string) (selectedRoute, error) {
	route, err := selectBindRoute(
		s.selector,
		s.ifaceBindings,
		s.ipCache,
		func(binding ifaceBinding) bool {
			if binding.sourceIP != nil {
				return true
			}
			return prefersIPv4Dial(addr)
		},
	)
	if err != nil {
		s.telemetry.ObserveConnection(route.ifaceName, false, 0, 0)
		return selectedRoute{}, err
	}

	return route, nil
}

func (s *SOCKSServer) buildDialer(serverCtx context.Context) func(context.Context, string, string) (net.Conn, error) {
	logger := toolLog.Of(serverCtx)

	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		id := s.connID.Add(1)
		route, targetConn, err := dialWithFailover(
			ctx,
			s.selector,
			s.ifaceBindings,
			s.ipCache,
			func(binding ifaceBinding) bool {
				if binding.sourceIP != nil {
					return true
				}
				return prefersIPv4Dial(addr)
			},
			failoverAttempts(s.cfg.FailoverAttempts, len(s.ifaceBindings)),
			func(ctx context.Context, bindIP net.IP) (net.Conn, error) {
				dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: bindIP}}
				return dialer.DialContext(ctx, network, addr)
			},
			func(route selectedRoute, _ error) {
				if route.ifaceName != "" {
					s.telemetry.ObserveConnection(route.ifaceName, false, 0, 0)
				}
			},
		)
		if err != nil {
			return nil, err
		}

		logger.Debug("socks connected target via interface",
			zap.Uint64("connection_id", id),
			zap.String("target", addr),
			zap.String("iface", route.ifaceName),
			zap.Int("iface_index", route.binding.index),
			zap.String("bind_ip", route.bindIP.String()),
		)

		return &monitoredSOCKSTargetConn{
			Conn:      targetConn,
			ifaceName: route.ifaceName,
			selector:  s.selector,
			telemetry: s.telemetry,
		}, nil
	}
}

func prefersIPv4Dial(addr string) bool {
	if addr == "" {
		return true
	}

	host := addr
	if host[0] == '[' {
		end := strings.LastIndexByte(host, ']')
		if end <= 0 {
			return true
		}
		host = host[1:end]
	} else {
		colon := strings.LastIndexByte(host, ':')
		if colon <= 0 {
			return true
		}
		host = host[:colon]
	}

	// IPv6 literals include ':'; parse them directly.
	if strings.IndexByte(host, ':') < 0 {
		// Hostnames almost always resolve via IPv4 in this proxy use-case; skip ParseIP on obvious names.
		for i := 0; i < len(host); i++ {
			ch := host[i]
			if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
				return true
			}
		}
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return true
	}
	return ip.To4() != nil
}

type socks5DebugLogWriter struct {
	logger *zap.Logger
}

func (w socks5DebugLogWriter) Write(p []byte) (int, error) {
	if w.logger == nil || !w.logger.Core().Enabled(zap.DebugLevel) {
		return len(p), nil
	}

	line := string(bytes.TrimSpace(p))
	if line == "" {
		return len(p), nil
	}

	w.logger.Debug("socks5", zap.String("line", line))
	return len(p), nil
}
