package apc

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"syscall"

	"gitlab.sovcombank.group/scb-mobile/lib/go-apc.git/pool"
	"go.uber.org/zap"
	"golang.org/x/text/encoding/charmap"
)

type Options struct {
	Addr   string
	Logger *zap.Logger
}

type Option func(*Options)

func DevelopmentLogger() Option {
	return func(options *Options) {
		options.Logger, _ = zap.NewDevelopment()
	}
}

func ProductionLogger() Option {
	return func(options *Options) {
		options.Logger, _ = zap.NewProduction()
	}
}

type Client struct {
	opts *Options

	conn net.Conn
	br   *bufio.Reader

	mu           sync.RWMutex
	invokeIDPool *pool.InvokeIDPool
	requests     map[uint32]chan Event
}

func NewClient(addr string, opts ...Option) (*Client, error) {
	options := &Options{
		Addr:   addr,
		Logger: zap.NewNop(),
	}

	for _, opt := range opts {
		opt(options)
	}

	cert, err := tls.LoadX509KeyPair("agentClient_cert.pem", "agentClient_key.pem")
	if err != nil {
		panic(err)
	}

	config := tls.Config{Certificates: []tls.Certificate{cert}, InsecureSkipVerify: true}
	conn, err := tls.Dial("tcp", addr, &config)
	if err != nil {
		return nil, fmt.Errorf("error while dialing host: %w", err)
	}

	decoder := charmap.Windows1251.NewDecoder().Reader(conn)
	br := bufio.NewReaderSize(decoder, 4096)

	c := &Client{
		opts:         options,
		conn:         conn,
		br:           br,
		invokeIDPool: pool.NewInvokeIDPool(),
		requests:     make(map[uint32]chan Event),
	}

	// Read AGTSTART
	event, err := c.readEvent()
	if err != nil {
		c.opts.Logger.Error("error while reading an event", zap.Error(err))
		return nil, err
	}

	// Check that first notification message is correct
	if event.Keyword != "AGTSTART" ||
		!event.IsStart() {
		c.opts.Logger.Error("server cannot accept new clients")
		return nil, err
	}

	return c, nil
}

func (c *Client) readEvent() (Event, error) {
	var rawEvent string
	for {
		char, _, err := c.br.ReadRune()
		if err != nil {
			return Event{}, err
		}

		rawEvent += string(char)

		if char == ETX || char == ETB {
			break
		}
	}

	c.opts.Logger.Debug("event has received", zap.String("raw", rawEvent))

	event, err := decodeEvent(rawEvent)
	if err != nil {
		return Event{}, err
	}

	c.opts.Logger.With(
		zap.String("keyword", event.Keyword),
		zap.ByteString("type", []byte{byte(event.Type)}),
		zap.String("client", event.Client),
		zap.Uint32("process_id", event.ProcessID),
		zap.Uint32("invoke_id", event.InvokeID),
		zap.Strings("segments", event.Segments),
		zap.Bool("incomplete", event.Incomplete),
	).Info("event has decoded")

	return event, nil
}

func (c *Client) readOnce() (connected bool, err error) {
	event, err := c.readEvent()
	// Logoff or connection closed
	if err == io.EOF {
		return false, nil
	} else if err != nil {
		if opErr, ok := err.(*net.OpError); ok {
			if syscallErr, ok := opErr.Err.(*os.SyscallError); ok {
				if syscallErr.Err == syscall.ECONNRESET {
					return false, nil
				}
			}
		}

		if IsDecodingError(err) {
			return true, err
		}

		return false, err
	}

	c.handleEvent(event)

	return true, nil
}

func (c *Client) handleEvent(event Event) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	eventChan, ok := c.requests[event.InvokeID]
	if !ok {
		return
	}

	eventChan <- event
}

func (c *Client) Start() {
	defer c.conn.Close()

	for {
		connected, err := c.readOnce()
		if err != nil {
			c.opts.Logger.Error("error while reading an event", zap.Error(err))
		}
		if !connected {
			return
		}
	}
}
