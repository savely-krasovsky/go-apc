package apc

import (
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"
)

func (c *Client) Logon(agentName string, password string) error {
	keyword := "AGTLogon"

	invokeID := c.invokeIDPool.Get()
	defer c.invokeIDPool.Release(invokeID)

	b, _ := encodeCommand(keyword, invokeID, agentName, password)
	c.opts.Logger.Debug("command has encoded", zap.ByteString("raw", b))

	if _, err := c.conn.Write(b); err != nil {
		return fmt.Errorf("cannot write command: %w", err)
	}
	c.opts.Logger.With(
		zap.String("keyword", keyword),
		zap.Uint32("invoke_id", invokeID),
		zap.String("agent_name", agentName),
		zap.String("password", password),
	).Info("command has sent")

	eventChan := make(chan *Event, 2)
	defer close(eventChan)

	c.mu.Lock()
	c.requests[invokeID] = eventChan
	c.mu.Unlock()

	// pending event
	<-eventChan
	// response event
	<-eventChan

	c.mu.Lock()
	delete(c.requests, invokeID)
	c.mu.Unlock()

	return nil
}

// Logoff sends ATGLogoff command, then Proactive Control server terminates session
func (c *Client) Logoff() error {
	keyword := "AGTLogoff"

	invokeID := c.invokeIDPool.Get()
	defer c.invokeIDPool.Release(invokeID)

	b, _ := encodeCommand(keyword, invokeID)
	c.opts.Logger.Debug("command has encoded", zap.ByteString("raw", b))

	if _, err := c.conn.Write(b); err != nil {
		return fmt.Errorf("cannot write command: %w", err)
	}
	c.opts.Logger.With(
		zap.String("keyword", keyword),
		zap.Uint32("invoke_id", invokeID),
	).Info("command has sent")

	eventChan := make(chan *Event, 1)
	defer close(eventChan)

	c.mu.Lock()
	c.requests[invokeID] = eventChan
	c.mu.Unlock()

	// pending event
	<-eventChan

	c.mu.Lock()
	delete(c.requests, invokeID)
	c.mu.Unlock()

	return nil
}

type JobType byte

const (
	JobTypeAll      JobType = 'A'
	JobTypeBlend    JobType = 'B'
	JobTypeOutbound JobType = 'O'
	JobTypeInbound  JobType = 'I'
	JobTypeManaged  JobType = 'M'
)

type Job struct {
	Type   JobType
	Name   string
	Status StatusType
}

type StatusType byte

const (
	StatusTypeInactive StatusType = 'I'
	StatusTypeActive   StatusType = 'A'
)

func (c *Client) ListJobs(jobType JobType) ([]Job, error) {
	keyword := "AGTListJobs"

	invokeID := c.invokeIDPool.Get()
	defer c.invokeIDPool.Release(invokeID)

	b, _ := encodeCommand(keyword, invokeID, string([]byte{byte(jobType)}))
	c.opts.Logger.Debug("command has encoded", zap.ByteString("raw", b))

	if _, err := c.conn.Write(b); err != nil {
		return nil, fmt.Errorf("cannot write command: %w", err)
	}
	c.opts.Logger.With(
		zap.String("keyword", keyword),
		zap.Uint32("invoke_id", invokeID),
		zap.ByteString("job_type", []byte{byte(jobType)}),
	).Info("command has sent")

	eventChan := make(chan *Event, 1)
	defer close(eventChan)

	c.mu.Lock()
	c.requests[invokeID] = eventChan
	c.mu.Unlock()

	// data event
	dataEvent := <-eventChan
	// response event
	<-eventChan

	c.mu.Lock()
	delete(c.requests, invokeID)
	c.mu.Unlock()

	if dataEvent.Type != EventTypeData {
		return nil, errors.New("incorrect event type")
	}
	if len(dataEvent.Segments) < 2 {
		return nil, errors.New("there are no data segments")
	}
	if dataEvent.Segments[0] != "0" || dataEvent.Segments[1] != "M00001" {
		return nil, errors.New("bad status")
	}

	jobs := make([]Job, 0, len(dataEvent.Segments[2:]))
	for _, segment := range dataEvent.Segments[2:] {
		jobParts := strings.Split(segment, ",")
		if len(jobParts) == 3 {
			jobs = append(jobs, Job{
				Type:   JobType(jobParts[0][0]),
				Name:   jobParts[1],
				Status: StatusType(jobParts[2][0]),
			})
		}
	}

	return jobs, nil
}
