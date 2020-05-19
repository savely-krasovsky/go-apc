package apc

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"sync"

	"gitlab.sovcombank.group/scb-mobile/lib/go-apc.git/pool"
	"go.uber.org/zap"
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

type Client struct {
	options *Options

	conn         net.Conn
	mu           sync.RWMutex
	invokeIDPool *pool.InvokeIDPool
	requests     map[uint32]chan *Event
	done         chan struct{}
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

	return &Client{
		options:      options,
		conn:         conn,
		invokeIDPool: pool.NewInvokeIDPool(),
		requests:     make(map[uint32]chan *Event),
	}, nil
}

func (c *Client) Start() {
	defer c.conn.Close()
	br := bufio.NewReader(c.conn)

	for {
		select {
		case <-c.done:
			return
		default:
			b, err := br.ReadBytes(ETX)
			if err != nil {
				// ReadBytes returns err != nil if and only if line does not end in delim.
				return
			}

			event, err := decodeEvent(b)
			if err != nil {
				c.options.Logger.Error("error while decoding event", zap.Error(err))
				return
			}

			c.options.Logger.With(
				zap.String("keyword", event.Keyword),
				zap.ByteString("type", []byte{byte(event.Type)}),
				zap.String("client", event.Client),
				zap.Uint32("process_id", event.ProcessID),
				zap.Uint32("invoke_id", event.InvokeID),
				zap.Strings("segments", event.Segments),
			).Info("event received")

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
}

func (c *Client) Stop() {
	c.done <- struct{}{}
}
