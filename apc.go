package apc

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sync"

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

	conn         net.Conn
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

	br := bufio.NewReaderSize(conn, 4096)

	c := &Client{
		opts:         options,
		conn:         conn,
		invokeIDPool: pool.NewInvokeIDPool(),
		requests:     make(map[uint32]chan Event),
	}

	// Consume AGTSTART
	event, err := c.consumeEvent(br)
	if err != nil {
		c.opts.Logger.Error("error while consuming an event", zap.Error(err))
		return nil, err
	}

	// Check that first notification message is correct
	if event.Keyword != "AGTSTART" ||
		event.CommandStatus != 0 ||
		event.MessageCode != "AGENT_STARTUP" {
		c.opts.Logger.Error("server cannot accept new clients")
		return nil, err
	}

	return c, nil
}

func (c *Client) consumeEvent(reader io.Reader) (Event, error) {
	decoder := charmap.Windows1251.NewDecoder().Reader(reader)
	br := bufio.NewReader(decoder)

	var atom string
	for {
		char, _, err := br.ReadRune()
		if err != nil {
			return Event{}, err
		}

		atom += string(char)

		if char == ETX || char == ETB {
			break
		}
	}

	c.opts.Logger.Debug("event has received", zap.String("raw", atom))

	event, err := decodeEvent(atom)
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
		zap.Uint8("command_status", event.CommandStatus),
		zap.String("message_code", string(event.MessageCode)),
	).Info("event has decoded")

	return event, nil
}

func (c *Client) Start() {
	defer c.conn.Close()
	br := bufio.NewReader(c.conn)

	for {
		event, err := c.consumeEvent(br)
		if err != nil {
			// Logoff or connection closed
			if err == io.EOF {
				return
			}

			c.opts.Logger.Error("error while consuming an event", zap.Error(err))
			continue
		}

		func() {
			c.mu.RLock()
			defer c.mu.RUnlock()

			eventChan, ok := c.requests[event.InvokeID]
			if !ok {
				return
			}

			eventChan <- event
		}()
	}
}
