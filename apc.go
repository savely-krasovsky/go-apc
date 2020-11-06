package apc

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"sync"
	"time"

	//"github.com/spacemonkeygo/openssl"
	"gitlab.sovcombank.group/scb-mobile/lib/go-apc.git/pool"
	"go.uber.org/atomic"
	"go.uber.org/zap"
	"golang.org/x/text/encoding/charmap"
)

type Options struct {
	Timeout          *time.Duration
	LogLevel         LogLevel
	LogHandler       LogHandler
	NativeHTTPClient bool
}

type Option func(*Options)

// WithTimeout returns an Option with Timeout for underlying Client connection.
func WithTimeout(timeout time.Duration) Option {
	return func(options *Options) {
		options.Timeout = &timeout
	}
}

// WithLogger returns an Option with zap logger (JSON).
func WithLogger() Option {
	return func(options *Options) {
		logger, _ := zap.NewDevelopment()

		options.LogLevel = LogLevelDebug
		options.LogHandler = func(entry LogEntry) {
			fields := make([]zap.Field, 0, len(entry.Fields))
			for k, v := range entry.Fields {
				fields = append(fields, zap.Any(k, v))
			}

			switch entry.Level {
			case LogLevelDebug:
				logger.With(fields...).Debug(entry.Message)
			case LogLevelInfo:
				logger.With(fields...).Info(entry.Message)
			case LogLevelError:
				logger.With(fields...).Error(entry.Message)
			case LogLevelNone:
			}
		}
	}
}

// WithLogHandler returns an Option with custom log handler.
func WithLogHandler(logLevel LogLevel, handler LogHandler) Option {
	return func(options *Options) {
		options.LogLevel = logLevel
		options.LogHandler = handler
	}
}

// WithNativeHTTPClient returns an Option that forces
// client to use Golang native HTTP client instead of OpenSSL.
func WithNativeHTTPClient() Option {
	return func(options *Options) {
		options.NativeHTTPClient = true
	}
}

const (
	ConnOK uint32 = iota
	ConnClosed
)

var (
	ErrConnectionClosed = errors.New("connection closed")
	ErrHelloNotReceived = errors.New("hello not received")
)

type request struct {
	context   context.Context
	cancel    context.CancelFunc
	eventChan chan Event
}

type Client struct {
	opts   *Options
	logger *logger

	state *atomic.Uint32

	conn          net.Conn
	events        chan Event
	notifications chan Notification
	shutdown      chan struct{}

	invokeIDPool *pool.InvokeIDPool
	requests     map[uint32]*request

	mu sync.RWMutex
}

// NewClient returns Avaya Proactive Client Agent API client to work with.
// Client keeps alive underlying connection, because APC proto is stateful.
func NewClient(addr string, opts ...Option) (*Client, error) {
	options := &Options{}

	// Apply passed opts
	for _, opt := range opts {
		opt(options)
	}

	var tlsConn net.Conn
	if !options.NativeHTTPClient {
		/*// Avaya Proactive Contact agent binary support only TLSv1
		sslCtx, err := openssl.NewCtxWithVersion(openssl.TLSv1)
		if err != nil {
			return nil, fmt.Errorf("error while initializing OpenSSL context: %w", err)
		}

		// It's just raw TLS, encrypted by session keys, there is no host verification
		tlsConn, err = openssl.Dial("tcp", addr, sslCtx, openssl.InsecureSkipHostVerification)
		if err != nil {
			return nil, fmt.Errorf("error while dialing: %w", err)
		}*/
	} else {
		// Golang native realization DO NOT WORK and I don't fucking know why. Seriously.
		// Server just drops connection after few requests/minutes with errno: -11 (EAGAIN or EWOULDBLOCK).
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("error while dialing: %w", err)
		}

		tlsConn = tls.Client(conn, &tls.Config{
			InsecureSkipVerify: true,
		})
	}

	c := &Client{
		opts:          options,
		state:         atomic.NewUint32(ConnOK),
		conn:          tlsConn,
		events:        make(chan Event, 128),
		notifications: make(chan Notification, 128),
		shutdown:      make(chan struct{}),
		invokeIDPool:  pool.NewInvokeIDPool(),
		requests:      make(map[uint32]*request),
	}
	if options.LogHandler != nil {
		c.logger = newLogger(options.LogLevel, options.LogHandler)
	}

	go func() {
		if err := c.readEvents(); err != nil {
			if err == io.EOF {
				c.logger.log(newLogEntry(LogLevelError, "EOF received!", map[string]interface{}{"error": err}))
			} else {
				c.logger.log(newLogEntry(LogLevelError, "Error received!", map[string]interface{}{"error": err}))
			}
		}

		c.shutdown <- struct{}{}
	}()

	// Read AGTSTART
	event := <-c.events

	// Check that first notification message is correct
	if event.Keyword != "AGTSTART" ||
		!event.IsStart() {
		c.logger.log(newLogEntry(LogLevelError, "Server cannot accept new clients!"))
		return nil, ErrHelloNotReceived
	}

	return c, nil
}

// Start starts main event loop handler.
func (c *Client) Start() error {
	for {
		select {
		case event := <-c.events:
			if event.Type == EventTypeNotification {
				event.InvokeID = math.MaxUint32
			}

			c.mu.RLock()
			r, ok := c.requests[event.InvokeID]
			c.mu.RUnlock()

			if ok {
				r.eventChan <- event
			}
		case <-c.shutdown:
			// In case of shutting down mark connection as closed...
			c.state.Store(ConnClosed)

			// Close it...
			c.conn.Close()

			// Close notifications channel...
			close(c.notifications)

			// Close global events channel...
			close(c.events)

			// And finally send done signal to all active requests.
			for _, r := range c.requests {
				r.cancel()
			}

			return ErrConnectionClosed
		}
	}
}

// Notifications returns read-only notification event channel.
func (c *Client) Notifications(ctx context.Context) <-chan Notification {
	r := newRequest(ctx)

	c.mu.Lock()
	c.requests[math.MaxUint32] = r
	c.mu.Unlock()

	go processNotifications(r, c.notifications)
	return c.notifications
}

func (c *Client) readEvents() error {
	// Server still uses Windows1251 as default encoding.
	decoder := charmap.Windows1251.NewDecoder().Reader(c.conn)

	// Main event loop.
	for {
		// Set actual
		if c.opts.Timeout != nil {
			if err := c.conn.SetReadDeadline(time.Now().Add(*c.opts.Timeout)); err != nil {
				c.logger.log(newLogEntry(LogLevelError, "Error while setting a deadline!", map[string]interface{}{"error": err}))
				return err
			}
		}

		// 4096 bytes is a maximum request size.
		buf := make([]byte, 4096)

		n, err := decoder.Read(buf)
		if err != nil {
			return err
		}

		// If the last byte of read buffer is ETX or ETB, then start event decoding
		if buf[n-1] == ETX || buf[n-1] == ETB {
			rawEvent := string(buf[:n])
			c.logger.log(newLogEntry(LogLevelDebug, "Event has received.", map[string]interface{}{"raw": rawEvent}))

			event, err := decodeEvent(rawEvent)
			if err != nil {
				c.logger.log(newLogEntry(LogLevelError, "Error while decoding an event!", map[string]interface{}{"error": err}))
				// We could ignore it and read newer events.
				continue
			}

			c.logger.log(newLogEntry(
				LogLevelInfo,
				"Event has decoded.",
				map[string]interface{}{
					"keyword":    event.Keyword,
					"type":       string(event.Type),
					"client":     event.Client,
					"process_id": event.ProcessID,
					"invoke_id":  event.InvokeID,
					"segments":   event.Segments,
					"incomplete": event.IsIncomplete,
				},
			))

			c.events <- event

			// In case of successful logoff just break the read loop
			if event.IsSuccessfulResponse() && event.Keyword == "AGTLogoff" {
				break
			}
		}
	}

	return nil
}
