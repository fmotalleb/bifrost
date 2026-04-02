package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"

	"github.com/fmotalleb/go-tools/log"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/fmotalleb/bifrost/config"
)

// Server is a TCP reverse proxy that binds each upstream connection to a selected interface.
type Server struct {
	cfg      config.Config
	selector *Selector
	connID   atomic.Uint64
}

// NewServer constructs a proxy server from config.
func NewServer(cfg config.Config) (*Server, error) {
	selector, err := NewSelector(cfg.IFaces)
	if err != nil {
		return nil, err
	}

	return &Server{cfg: cfg, selector: selector}, nil
}

// Serve starts listening and blocks until context cancellation or fatal listener failure.
func (s *Server) Serve(ctx context.Context) error {
	logger := log.Of(ctx)
	listener, err := net.Listen("tcp", s.cfg.Listen.String())
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.cfg.Listen, err)
	}
	defer listener.Close()

	logger.Info("listening",
		zap.String("listen", s.cfg.Listen.String()),
		zap.String("server", s.cfg.Server.String()),
	)

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		clientConn, acceptErr := listener.Accept()
		if acceptErr != nil {
			if errors.Is(acceptErr, net.ErrClosed) || ctx.Err() != nil {
				return nil
			}

			logger.Warn("accept failed", zap.Error(acceptErr))
			continue
		}

		id := s.connID.Add(1)
		go s.handleConnection(ctx, id, clientConn)
	}
}

func (s *Server) handleConnection(ctx context.Context, id uint64, clientConn net.Conn) {
	logger := log.Of(ctx)
	defer clientConn.Close()

	ifaceName, err := s.selector.Pick()
	if err != nil {
		logger.Warn("failed to select interface", zap.Uint64("connection_id", id), zap.Error(err))
		return
	}

	bindIP, err := ResolveBindIP(ifaceName, s.cfg.Server.Addr().Is4())
	if err != nil {
		logger.Warn("failed to resolve interface ip",
			zap.Uint64("connection_id", id),
			zap.String("iface", ifaceName),
			zap.Error(err),
		)
		return
	}

	dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: bindIP}}
	upstreamConn, err := dialer.DialContext(ctx, "tcp", s.cfg.Server.String())
	if err != nil {
		logger.Warn("failed to dial upstream",
			zap.Uint64("connection_id", id),
			zap.String("iface", ifaceName),
			zap.String("bind_ip", bindIP.String()),
			zap.Error(err),
		)
		return
	}
	defer upstreamConn.Close()

	logger.Info("accepted connection",
		zap.Uint64("connection_id", id),
		zap.String("client", clientConn.RemoteAddr().String()),
		zap.String("iface", ifaceName),
		zap.String("bind_ip", bindIP.String()),
		zap.String("upstream", s.cfg.Server.String()),
	)

	if proxyErr := pipeBothWays(clientConn, upstreamConn); proxyErr != nil {
		fields := []zap.Field{
			zap.Uint64("connection_id", id),
			zap.String("client", clientConn.RemoteAddr().String()),
			zap.String("upstream", s.cfg.Server.String()),
			zap.Error(proxyErr),
		}
		if isHotPathConnectionError(proxyErr) {
			logger.Debug("proxy stream closed with expected network error", fields...)
			return
		}
		logger.Warn("proxy stream failed", fields...)
	}
}

func pipeBothWays(clientConn, upstreamConn net.Conn) error {
	group := new(errgroup.Group)

	group.Go(func() error {
		return copyAndCloseWrite(upstreamConn, clientConn, "client_to_upstream")
	})

	group.Go(func() error {
		return copyAndCloseWrite(clientConn, upstreamConn, "upstream_to_client")
	})

	return group.Wait()
}

func copyAndCloseWrite(dst, src net.Conn, direction string) error {
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("%s: copy: %w", direction, err)
	}

	if err := closeWrite(dst); err != nil {
		return fmt.Errorf("%s: close write: %w", direction, err)
	}

	return nil
}

func closeWrite(conn net.Conn) error {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return conn.Close()
	}

	return tcpConn.CloseWrite()
}

func isHotPathConnectionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		switch opErr.Op {
		case "read", "write", "close":
			return true
		}
	}

	return false
}
