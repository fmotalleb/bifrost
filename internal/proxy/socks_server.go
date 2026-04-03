package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	normalizedIFaces, bindings, err := normalizeConfiguredIFaces(cfg.IFaces, true)
	if err != nil {
		return nil, fmt.Errorf("normalize interfaces: %w", err)
	}
	cfg.IFaces = normalizedIFaces

	cache, err := NewIPCache(cfg.Cache.TTL, cfg.Cache.Prefetch, bindings, true)
	if err != nil {
		return nil, fmt.Errorf("create ip cache: %w", err)
	}

	selector, err := NewSelector(cfg.IFaces)
	if err != nil {
		return nil, err
	}
	if telemetry == nil {
		telemetry = NoopTelemetry
	}

	return &SOCKSServer{
		cfg:           cfg,
		selector:      selector,
		ifaceBindings: bindings,
		ipCache:       cache,
		telemetry:     telemetry,
	}, nil
}

// Serve starts listening for SOCKS5 clients.
func (s *SOCKSServer) Serve(ctx context.Context) error {
	logger := toolLog.Of(ctx)
	if !s.cfg.Socks.Listen.IsValid() {
		return errors.New("socks.listen must be a valid address:port")
	}

	serverCfg := &socks5.Config{
		Logger: log.New(io.Discard, "", 0),
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

func (s *SOCKSServer) buildDialer(serverCtx context.Context) func(context.Context, string, string) (net.Conn, error) {
	logger := toolLog.Of(serverCtx)

	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		id := s.connID.Add(1)
		ifaceName, err := s.selector.Pick()
		if err != nil {
			s.telemetry.ObserveConnection("", false, 0, 0)
			return nil, fmt.Errorf("select interface: %w", err)
		}

		binding, ok := s.ifaceBindings[ifaceName]
		if !ok {
			s.selector.Release(ifaceName)
			s.telemetry.ObserveConnection(ifaceName, false, 0, 0)
			return nil, fmt.Errorf("missing cached interface binding for %q", ifaceName)
		}

		preferIPv4 := prefersIPv4Dial(addr)
		bindIP, err := s.ipCache.GetBindIP(binding, preferIPv4)
		if err != nil {
			s.selector.Release(ifaceName)
			s.telemetry.ObserveConnection(ifaceName, false, 0, 0)
			return nil, fmt.Errorf("resolve bind ip for %q: %w", ifaceName, err)
		}

		dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: bindIP}}
		targetConn, err := dialer.DialContext(ctx, network, addr)
		if err != nil {
			s.selector.Release(ifaceName)
			s.telemetry.ObserveConnection(ifaceName, false, 0, 0)
			return nil, err
		}

		logger.Debug("socks connected target via interface",
			zap.Uint64("connection_id", id),
			zap.String("target", addr),
			zap.String("iface", ifaceName),
			zap.Int("iface_index", binding.index),
			zap.String("bind_ip", bindIP.String()),
		)

		return &monitoredSOCKSTargetConn{
			Conn:      targetConn,
			ifaceName: ifaceName,
			selector:  s.selector,
			telemetry: s.telemetry,
		}, nil
	}
}

func prefersIPv4Dial(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return true
	}
	return ip.To4() != nil
}
