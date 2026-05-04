package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/armon/go-socks5"
	"github.com/elazarl/goproxy"
	toolLog "github.com/fmotalleb/go-tools/log"
	"github.com/google/uuid"
	"github.com/soheilhy/cmux"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/fmotalleb/bifrost/config"
)

// MixedProxyServer is a mixed HTTP and SOCKS5 proxy that binds outbound
// connections to selected interfaces.
type MixedProxyServer struct {
	cfg           config.Config
	selector      *Selector
	ifaceBindings map[string]ifaceBinding
	ipCache       *IPCache
	telemetry     Telemetry
}

// SOCKSServer is kept as a compatibility alias for the older SOCKS-only API.
type SOCKSServer = MixedProxyServer

// NewMixedProxyServer constructs a mixed HTTP/SOCKS5 proxy server from config.
func NewMixedProxyServer(cfg config.Config, telemetry Telemetry) (*MixedProxyServer, error) {
	runtime, err := prepareRuntimeDependencies(cfg, true, telemetry)
	if err != nil {
		return nil, err
	}

	return &MixedProxyServer{
		cfg:           runtime.cfg,
		selector:      runtime.selector,
		ifaceBindings: runtime.bindings,
		ipCache:       runtime.cache,
		telemetry:     runtime.telemetry,
	}, nil
}

// NewSOCKSServer constructs the mixed HTTP/SOCKS5 proxy server using the
// existing SOCKS command/config surface.
func NewSOCKSServer(cfg config.Config, telemetry Telemetry) (*SOCKSServer, error) {
	return NewMixedProxyServer(cfg, telemetry)
}

// Serve starts listening for mixed HTTP and SOCKS5 clients.
func (s *MixedProxyServer) Serve(ctx context.Context) error {
	logger := toolLog.Of(ctx)
	if !s.cfg.Socks.Listen.IsValid() {
		return errors.New("socks.listen must be a valid address:port")
	}

	socksServer, err := s.buildSOCKS5Server(ctx)
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Handler: s.buildHTTPProxyHandler(ctx),
	}

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp", s.cfg.Socks.Listen.String())
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.cfg.Socks.Listen, err)
	}
	defer listener.Close()

	logger.Info("mixed proxy listening",
		zap.String("listen", s.cfg.Socks.Listen.String()),
		zap.Strings("protocols", []string{"http", "socks5"}),
	)

	go func() {
		<-ctx.Done()
		_ = httpServer.Close()
		_ = listener.Close()
	}()

	mux := cmux.New(listener)
	httpListener := mux.Match(cmux.HTTP1Fast())
	socksListener := mux.Match(cmux.Any())

	group := new(errgroup.Group)
	group.Go(func() error {
		err := httpServer.Serve(httpListener)
		if isExpectedProxyServerClose(err, ctx) {
			return nil
		}
		return fmt.Errorf("serve http proxy listener: %w", err)
	})
	group.Go(func() error {
		err := socksServer.Serve(socksListener)
		if isExpectedProxyServerClose(err, ctx) {
			return nil
		}
		return fmt.Errorf("serve socks5 proxy listener: %w", err)
	})
	group.Go(func() error {
		err := mux.Serve()
		if isExpectedProxyServerClose(err, ctx) {
			return nil
		}
		return fmt.Errorf("serve mixed proxy listener: %w", err)
	})

	return group.Wait()
}

func (s *MixedProxyServer) buildSOCKS5Server(ctx context.Context) (*socks5.Server, error) {
	logger := toolLog.Of(ctx)

	serverCfg := &socks5.Config{
		Logger: log.New(socks5DebugLogWriter{logger: logger}, "", 0),
		Dial:   s.buildDialer(ctx, "socks5"),
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
		return nil, fmt.Errorf("create socks5 server: %w", err)
	}

	return server, nil
}

func (s *MixedProxyServer) buildHTTPProxyHandler(ctx context.Context) http.Handler {
	proxyServer := goproxy.NewProxyHttpServer()
	proxyServer.Verbose = false
	proxyServer.Tr = &http.Transport{
		Proxy:             nil,
		DialContext:       s.buildDialer(ctx, "http"),
		DisableKeepAlives: true,
	}
	proxyServer.ConnectDialWithReq = func(req *http.Request, network, addr string) (net.Conn, error) {
		dialCtx := ctx
		if req != nil {
			dialCtx = req.Context()
		}
		return s.dialTarget(dialCtx, network, addr, "http_connect", "")
	}

	return proxyServer
}

type monitoredTargetConn struct {
	net.Conn
	ifaceName string
	selector  *Selector
	telemetry Telemetry
	released  sync.Once
	txBytes   atomic.Int64
	rxBytes   atomic.Int64
}

func (c *monitoredTargetConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 {
		c.txBytes.Add(int64(n))
		c.telemetry.AddTransfer(c.ifaceName, DirectionTX, int64(n))
	}
	return n, err
}

func (c *monitoredTargetConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.rxBytes.Add(int64(n))
		c.telemetry.AddTransfer(c.ifaceName, DirectionRX, int64(n))
	}
	return n, err
}

func (c *monitoredTargetConn) CloseWrite() error {
	closeWriter, ok := c.Conn.(interface{ CloseWrite() error })
	if !ok {
		return c.Conn.Close()
	}
	return closeWriter.CloseWrite()
}

func (c *monitoredTargetConn) Close() error {
	err := c.Conn.Close()
	c.release(true)
	return err
}

func (c *monitoredTargetConn) release(success bool) {
	c.released.Do(func() {
		c.selector.Release(c.ifaceName)
		c.telemetry.AddActiveConnections(c.ifaceName, -1)
		c.telemetry.ObserveConnection(c.ifaceName, success, c.txBytes.Load(), c.rxBytes.Load())
	})
}

func (s *MixedProxyServer) selectDialRoute(addr string) (selectedRoute, error) {
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
		nil,
		"",
	)
	if err != nil {
		s.telemetry.ObserveConnection(route.ifaceName, false, 0, 0)
		return selectedRoute{}, err
	}

	return route, nil
}

func (s *MixedProxyServer) buildDialer(
	serverCtx context.Context,
	protocol string,
) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialCtx := ctx
		if dialCtx == nil {
			dialCtx = serverCtx
		}
		connectionID := uuid.NewString()
		connLogger := toolLog.Of(dialCtx).With(
			zap.String("connection_id", connectionID),
			zap.String("protocol", protocol),
			zap.String("network", network),
			zap.String("target", addr),
		)
		dialCtx = toolLog.WithLogger(dialCtx, connLogger)
		connLogger.Debug("client request initiated")
		return s.dialTarget(dialCtx, network, addr, protocol, connectionID)
	}
}

func (s *MixedProxyServer) dialTarget(
	ctx context.Context,
	network, addr, protocol, connectionID string,
) (net.Conn, error) {
	logger := toolLog.Of(ctx)
	if connectionID == "" {
		connectionID = uuid.NewString()
		logger = logger.With(
			zap.String("connection_id", connectionID),
			zap.String("protocol", protocol),
			zap.String("network", network),
			zap.String("target", addr),
		)
		ctx = toolLog.WithLogger(ctx, logger)
	}
	logger.Debug("connection lifecycle started")

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
		func(ctx context.Context, route selectedRoute) (net.Conn, error) {
			return dialContextOnRoute(ctx, network, addr, route)
		},
		func(route selectedRoute, _ error) {
			if route.ifaceName != "" {
				s.telemetry.ObserveConnection(route.ifaceName, false, 0, 0)
			}
		},
		logger,
		connectionID,
	)
	if err != nil {
		return nil, err
	}

	logger.Debug("proxy connected target via interface",
		zap.String("iface", route.ifaceName),
		zap.Int("iface_index", route.binding.index),
		zap.String("bind_ip", route.bindIP.String()),
	)
	s.telemetry.AddActiveConnections(route.ifaceName, 1)

	return &monitoredTargetConn{
		Conn:      targetConn,
		ifaceName: route.ifaceName,
		selector:  s.selector,
		telemetry: s.telemetry,
	}, nil
}

func isExpectedProxyServerClose(err error, ctx context.Context) bool {
	if err == nil || ctx.Err() != nil {
		return true
	}
	if errors.Is(err, net.ErrClosed) || errors.Is(err, http.ErrServerClosed) {
		return true
	}

	return strings.Contains(err.Error(), "use of closed network connection")
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
