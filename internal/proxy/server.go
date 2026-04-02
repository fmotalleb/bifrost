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

var errStreamAborted = errors.New("proxy stream aborted")

// Server is a TCP reverse proxy that binds each upstream connection to a selected interface.
type Server struct {
	cfg              config.Config
	selector         *Selector
	ifaceIndexByName map[string]int
	connID           atomic.Uint64
}

// NewServer constructs a proxy server from config.
func NewServer(cfg config.Config) (*Server, error) {
	normalizedIFaces, ifaceIndexes, err := normalizeConfiguredIFaces(cfg.IFaces)
	if err != nil {
		return nil, fmt.Errorf("normalize interfaces: %w", err)
	}
	cfg.IFaces = normalizedIFaces

	selector, err := NewSelector(cfg.IFaces)
	if err != nil {
		return nil, err
	}

	return &Server{
		cfg:              cfg,
		selector:         selector,
		ifaceIndexByName: ifaceIndexes,
	}, nil
}

func normalizeConfiguredIFaces(
	ifaces map[string]config.Iface,
) (map[string]config.Iface, map[string]int, error) {
	normalized := make(map[string]config.Iface, len(ifaces))
	indexes := make(map[string]int, len(ifaces))
	for configuredName, ifaceCfg := range ifaces {
		resolvedIface, err := ResolveInterface(configuredName)
		if err != nil {
			return nil, nil, err
		}
		resolvedName := resolvedIface.Name

		if _, exists := normalized[resolvedName]; exists {
			return nil, nil, fmt.Errorf(
				"configured interfaces resolve to the same OS interface %q",
				resolvedName,
			)
		}

		normalized[resolvedName] = ifaceCfg
		indexes[resolvedName] = resolvedIface.Index
	}

	return normalized, indexes, nil
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

	ifaceIndex, ok := s.ifaceIndexByName[ifaceName]
	if !ok {
		logger.Warn("missing cached interface index",
			zap.Uint64("connection_id", id),
			zap.String("iface", ifaceName),
		)
		return
	}

	bindIP, err := ResolveBindIPByIndex(ifaceIndex, s.cfg.Server.Addr().Is4())
	if err != nil {
		logger.Warn("failed to resolve interface ip",
			zap.Uint64("connection_id", id),
			zap.String("iface", ifaceName),
			zap.Int("iface_index", ifaceIndex),
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
			zap.Int("iface_index", ifaceIndex),
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
		zap.Int("iface_index", ifaceIndex),
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
	results := make(chan streamCopyResult, 2)

	group.Go(func() error {
		results <- copyAndCloseWrite(upstreamConn, clientConn, "client_to_upstream")
		return nil
	})

	group.Go(func() error {
		results <- copyAndCloseWrite(clientConn, upstreamConn, "upstream_to_client")
		return nil
	})

	_ = group.Wait()
	close(results)

	var aborted bool
	unexpected := make([]error, 0, 2)
	for result := range results {
		if result.aborted {
			aborted = true
		}
		if result.err != nil {
			unexpected = append(unexpected, result.err)
		}
	}

	if len(unexpected) > 0 {
		return errors.Join(unexpected...)
	}
	if aborted {
		return errStreamAborted
	}

	return nil
}

type streamCopyResult struct {
	aborted bool
	err     error
}

func copyAndCloseWrite(dst, src net.Conn, direction string) streamCopyResult {
	result := streamCopyResult{}

	if _, err := io.Copy(dst, src); err != nil {
		if isHotPathConnectionError(err) {
			result.aborted = true
		} else {
			result.err = fmt.Errorf("%s: copy: %w", direction, err)
		}
	}

	if err := closeWrite(dst); err != nil {
		if isHotPathConnectionError(err) {
			result.aborted = true
		} else if result.err == nil {
			result.err = fmt.Errorf("%s: close write: %w", direction, err)
		} else {
			result.err = errors.Join(result.err, fmt.Errorf("%s: close write: %w", direction, err))
		}
	}

	return result
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
	if errors.Is(err, errStreamAborted) {
		return true
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
