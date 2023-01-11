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

	tlsPatched "github.com/L11R/apc-tls"
	"github.com/L11R/go-apc/pool"
	"go.uber.org/atomic"
	"go.uber.org/zap"
	"golang.org/x/text/encoding"
)

type Options struct {
	Timeout       *time.Duration
	LogLevel      LogLevel
	LogHandler    LogHandler
	Decoder       *encoding.Decoder
	TlsPatched    bool
	TlsSkipVerify bool
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

// WithDecoder returns an Option with custom decoder
// e.g w/ charmap.Windows1251.NewDecoder().
func WithDecoder(decoder *encoding.Decoder) Option {
	return func(options *Options) {
		options.Decoder = decoder
	}
}

// WithTlsPatched returns an Option with patched TLS package to fix issues with old TLS 1.0 only Avaya server
func WithTlsPatched() Option {
	return func(options *Options) {
		options.TlsPatched = true
	}
}

// WithTlsSkipVerify returns an Option with flag to skip TLS verification (insecure!)
func WithTlsSkipVerify() Option {
	return func(options *Options) {
		options.TlsSkipVerify = true
	}
}

const (
	// ConnOK means that connection is currently online
	ConnOK uint32 = iota
	// ConnClosed means that connection is currently closing or already closed
	ConnClosed
)

var (
	ErrConnectionClosed = errors.New("connection closed")
	ErrHelloNotReceived = errors.New("hello not received")
)

// request is the private struct that represents a request to an APC server
type request struct {
	// context and cancel func to control a cancellation process
	context context.Context
	cancel  context.CancelFunc
	// each request has own event channel w/ a bunch of possible responses
	eventChan chan Event
}

type Client struct {
	opts   *Options
	logger *logger

	// Stores a current state of an underlying connection, e.g. ConnOK or ConnClosed
	state *atomic.Uint32

	// underlying connection
	conn net.Conn
	// decoder to deal with old encodings like Windows-1251
	decoder io.Reader
	// channel w/ decoded events that were received from a connection
	events chan Event
	// dedicated channel for notification events only
	notifications chan Notification
	// channel to shut down the *Client when the time will come
	shutdown chan error

	// a pool of invoke ids that are used by requests map
	//
	// Each method execution requires own invoke ID; for example a user of this library wants to execute
	// two methods at the same time, then this pool will give two invoke IDs: 1 and 2;
	// after execution they will be released for further use.
	invokeIDPool *pool.InvokeIDPool
	// a map that contains a set of currently executing requests
	requests map[uint32]*request
	// a mutex to control an access to requests map
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

	// Initiate the TCP connection to an APC server
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("error while dialing: %w", err)
	}

	// Use patched tls package (w/ disabled BEAST attack mitigation) to wrap the TCP connection;
	// Otherwise old APC server has random disconnects after a dozen of consistent writes.
	var tlsConn net.Conn
	if options.TlsPatched {
		tlsConn = tlsPatched.Client(conn, &tlsPatched.Config{
			AvayaCompatibility: true,
			InsecureSkipVerify: options.TlsSkipVerify,
			MinVersion:         tls.VersionTLS10,
		})
	} else {
		tlsConn = tls.Client(conn, &tls.Config{
			InsecureSkipVerify: options.TlsSkipVerify,
		})
	}

	c := &Client{
		opts:         options,
		state:        atomic.NewUint32(ConnOK),
		conn:         tlsConn,
		decoder:      tlsConn,
		events:       make(chan Event),
		shutdown:     make(chan error),
		invokeIDPool: pool.NewInvokeIDPool(),
		requests:     make(map[uint32]*request),
	}
	if options.Decoder != nil {
		c.decoder = options.Decoder.Reader(tlsConn)
	}
	if options.LogHandler != nil {
		c.logger = newLogger(options.LogLevel, options.LogHandler)
	}

	// Goroutine that starts event reading from the connection
	go func() {
		c.shutdown <- c.readEvents()
	}()

	// Read the first AGTSTART event before returning the *Client
	event := <-c.events

	// Check that the first notification message is correct
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
		// Wait for events, error or an execution of Stop()
		select {
		case event := <-c.events:
			// Assign notification events own invoke IDs to get them processed
			if event.Type == EventTypeNotification {
				event.InvokeID = math.MaxUint32
			}

			// Look up for a request
			c.mu.RLock()
			r, ok := c.requests[event.InvokeID]
			c.mu.RUnlock()

			// In case of success, send received event into own request event channel
			if ok {
				r.eventChan <- event
			}
		case err := <-c.shutdown:
			// In case of shutting down mark connection as closed...
			c.state.Store(ConnClosed)

			// Close it...
			if err := c.conn.Close(); err != nil {
				return err
			}

			// Close notifications channel...
			if c.notifications != nil {
				close(c.notifications)
			}

			// Close global events channel...
			close(c.events)

			// And finally send done signal to all active requests.
			func() {
				c.mu.RLock()
				defer c.mu.RUnlock()
				for _, r := range c.requests {
					r.cancel()
				}
			}()

			return err
		}
	}
}

// Notifications returns read-only notification event channel.
func (c *Client) Notifications(ctx context.Context) <-chan Notification {
	c.notifications = make(chan Notification, 128)

	// Notifications has own request...
	r := newRequest(ctx)

	// ...inside request map, but it has fake invoke ID to avoid conflicts with real ones.
	// Real invoke IDs are limited to 4 digits (9999), while MaxUint32 is 4294967295.
	c.mu.Lock()
	c.requests[math.MaxUint32] = r
	c.mu.Unlock()

	go func() {
		// Don't forget to delete it from map to avoid deadlock while notifications are not in use
		defer func() {
			c.mu.Lock()
			delete(c.requests, math.MaxUint32)
			c.mu.Unlock()
		}()

		processNotifications(r, c.notifications)
	}()

	return c.notifications
}

func (c *Client) readEvents() error {
	// Main event loop.
	for {
		// Set actual
		if c.opts.Timeout != nil {
			if err := c.conn.SetReadDeadline(time.Now().Add(*c.opts.Timeout)); err != nil {
				c.logger.log(newLogEntry(LogLevelError, "Error while setting a deadline!", map[string]interface{}{"error": err}))
				return err
			}
		}

		// 4096 bytes is the maximum request size, but 256 should be enough;
		// could be increased in case of getting errors.
		buf := make([]byte, 256)

		// Without decoder, it will use c.tlsConn directly; read through decoder to avoid encoding problems
		// (to activate it use WithDecoder()); for example in Russia APC server uses Windows-1251.
		n, err := c.decoder.Read(buf)
		if err != nil {
			if err == io.EOF {
				c.logger.log(newLogEntry(LogLevelInfo, "EOF received.", map[string]interface{}{"error": err}))
				return ErrConnectionClosed
			}

			c.logger.log(newLogEntry(LogLevelError, "Error received!", map[string]interface{}{"error": err}))
			return err
		}

		// If the last byte of read buffer is ETX or ETB, then start event decoding
		if buf[n-1] == ETX || buf[n-1] == ETB {
			rawEvent := string(buf[:n])
			c.logger.log(newLogEntry(LogLevelDebug, "Event has received.", map[string]interface{}{"raw": rawEvent}))

			event, err := decodeEvent(rawEvent)
			if err != nil {
				c.logger.log(newLogEntry(LogLevelError, "Error while decoding an event!", map[string]interface{}{"error": err}))
				return err
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
