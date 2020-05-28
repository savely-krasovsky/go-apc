package apc

import (
	"fmt"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

func (c *Client) invokeCommand(keyword string, args map[string]string) (<-chan Event, uint32, error) {
	invokeID := c.invokeIDPool.Get()

	zapFields := make([]zap.Field, 0, len(args)+2)
	zapFields = append(zapFields, zap.String("keyword", keyword), zap.Uint32("invoke_id", invokeID))

	var flatArgs []string
	if len(args) > 0 {
		flatArgs = make([]string, 0, len(args))
		for name, arg := range args {
			flatArgs = append(flatArgs, arg)
			zapFields = append(zapFields, zap.String(name, arg))
		}
	}

	// Encode command
	b, err := encodeCommand(keyword, invokeID, flatArgs...)
	if err != nil {
		return nil, invokeID, fmt.Errorf("cannot encode command: %w", err)
	}
	c.opts.Logger.Debug("command has encoded", zap.ByteString("raw", b))

	// Write command to connection
	if _, err := c.conn.Write(b); err != nil {
		return nil, invokeID, fmt.Errorf("cannot write command: %w", err)
	}
	c.opts.Logger.With(zapFields...).Info("command has sent")

	// Create dedicated channel for this request
	eventChan := make(chan Event, 1)

	c.mu.Lock()
	c.requests[invokeID] = eventChan
	c.mu.Unlock()

	return eventChan, invokeID, nil
}

func (c *Client) destroyCommand(invokeID uint32) {
	c.mu.Lock()
	eventChan, ok := c.requests[invokeID]
	c.mu.Unlock()

	// in case of executeCommand func returned an error just release invoke id from pool
	if !ok {
		c.invokeIDPool.Release(invokeID)
		return
	}

	close(eventChan)

	c.mu.Lock()
	delete(c.requests, invokeID)
	c.mu.Unlock()

	c.invokeIDPool.Release(invokeID)
}

func (c *Client) Logon(agentName string, password string) error {
	eventChan, invokeID, err := c.invokeCommand("AGTLogon", map[string]string{
		"agent_name": agentName,
		"password":   password,
	})
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTLogon command: %w", err)
	}

	for event := range eventChan {
		if event.IsPending() {
			continue
		}
		if event.IsComplete() {
			break
		}

		return fmt.Errorf("unexpected event")
	}

	return nil
}

func (c *Client) ReserveHeadset(headsetID int) error {
	eventChan, invokeID, err := c.invokeCommand("AGTReserveHeadset", map[string]string{
		"headset_id": strconv.Itoa(headsetID),
	})
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTReserveHeadset command: %w", err)
	}

	for event := range eventChan {
		if event.IsPending() {
			continue
		}
		if event.IsComplete() {
			break
		}

		return fmt.Errorf("unexpected event")
	}

	return nil
}

func (c *Client) ConnHeadset() error {
	eventChan, invokeID, err := c.invokeCommand("AGTConnHeadset", nil)
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTConnHeadset command: %w", err)
	}

	for event := range eventChan {
		if event.IsPending() {
			continue
		}
		if event.IsComplete() {
			break
		}

		return fmt.Errorf("unexpected event")
	}

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
	eventChan, invokeID, err := c.invokeCommand("AGTListJobs", map[string]string{
		"job_type": string([]byte{byte(jobType)}),
	})
	defer c.destroyCommand(invokeID)
	if err != nil {
		return nil, fmt.Errorf("error while executing AGTListJobs command: %w", err)
	}

	var dataSegments []string
	for event := range eventChan {
		if event.IsComplete() {
			continue
		}
		if event.Type == EventTypeData {
			dataSegments = append(dataSegments, event.Segments...)
			if event.Incomplete {
				continue
			}
			break
		}

		return nil, fmt.Errorf("unexpected event")
	}

	jobs := make([]Job, 0, len(dataSegments))
	for _, segment := range dataSegments {
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

func (c *Client) ListCallLists() ([]string, error) {
	eventChan, invokeID, err := c.invokeCommand("AGTListCallLists", nil)
	defer c.destroyCommand(invokeID)
	if err != nil {
		return nil, fmt.Errorf("error while executing AGTListCallLists command: %w", err)
	}

	var dataSegments []string
	for event := range eventChan {
		if event.IsComplete() {
			continue
		}
		if event.Type == EventTypeData {
			dataSegments = append(dataSegments, event.Segments...)
			if event.Incomplete {
				continue
			}
			break
		}

		return nil, fmt.Errorf("unexpected event")
	}

	callLists := make([]string, 0, len(dataSegments))
	for _, segment := range dataSegments {
		callLists = append(callLists, segment)
	}

	return callLists, nil
}

func (c *Client) ListCallFields(listName string) ([]string, error) {
	eventChan, invokeID, err := c.invokeCommand("AGTListCallFields", map[string]string{
		"list_name": listName,
	})
	defer c.destroyCommand(invokeID)
	if err != nil {
		return nil, fmt.Errorf("error while executing AGTListCallFields command: %w", err)
	}

	var dataSegments []string
	for event := range eventChan {
		if event.IsComplete() {
			continue
		}
		if event.Type == EventTypeData {
			dataSegments = append(dataSegments, event.Segments...)
			if event.Incomplete {
				continue
			}
			break
		}

		return nil, fmt.Errorf("unexpected event")
	}

	callFields := make([]string, 0, len(dataSegments))
	for _, segment := range dataSegments {
		callFields = append(callFields, segment)
	}

	return callFields, nil
}

func (c *Client) AttachJob(jobName string) error {
	eventChan, invokeID, err := c.invokeCommand("AGTAttachJob", map[string]string{
		"job_name": jobName,
	})
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTAttachJob command: %w", err)
	}

	for event := range eventChan {
		if event.IsPending() {
			continue
		}
		if event.IsComplete() {
			break
		}

		return fmt.Errorf("unexpected event")
	}

	return nil
}

type ListType byte

const (
	ListTypeOutbound ListType = 'O'
	ListTypeInbound  ListType = 'I'
)

func (c *Client) ListDataFields(listType ListType) ([]string, error) {
	eventChan, invokeID, err := c.invokeCommand("AGTListDataFields", map[string]string{
		"list_type": string([]byte{byte(listType)}),
	})
	defer c.destroyCommand(invokeID)
	if err != nil {
		return nil, fmt.Errorf("error while executing AGTListDataFields command: %w", err)
	}

	var dataSegments []string
	for event := range eventChan {
		if event.IsComplete() {
			continue
		}
		if event.Type == EventTypeData {
			dataSegments = append(dataSegments, event.Segments...)
			if event.Incomplete {
				continue
			}
			break
		}

		return nil, fmt.Errorf("unexpected event")
	}

	dataFields := make([]string, 0, len(dataSegments))
	for _, segment := range dataSegments {
		dataFields = append(dataFields, segment)
	}

	return dataFields, nil
}

func (c *Client) SetNotifyKeyField(listType ListType, fieldName string) error {
	eventChan, invokeID, err := c.invokeCommand("AGTSetNotifyKeyField", map[string]string{
		"list_type":  string([]byte{byte(listType)}),
		"field_name": fieldName,
	})
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTSetNotifyKeyField command: %w", err)
	}

	for event := range eventChan {
		if event.IsComplete() {
			break
		}

		return fmt.Errorf("unexpected event")
	}

	return nil
}

func (c *Client) SetDataField(listType ListType, fieldName string) error {
	eventChan, invokeID, err := c.invokeCommand("AGTSetDataField", map[string]string{
		"list_type":  string([]byte{byte(listType)}),
		"field_name": fieldName,
	})
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTSetDataField command: %w", err)
	}

	for event := range eventChan {
		if event.IsComplete() {
			break
		}

		return fmt.Errorf("unexpected event")
	}

	return nil
}

func (c *Client) AvailWork() error {
	eventChan, invokeID, err := c.invokeCommand("AGTAvailWork", nil)
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTAvailWork command: %w", err)
	}

	for event := range eventChan {
		if event.IsPending() {
			continue
		}
		if event.IsComplete() {
			break
		}

		return fmt.Errorf("unexpected event")
	}

	return nil
}

func (c *Client) ReadyNextItem() error {
	eventChan, invokeID, err := c.invokeCommand("AGTReadyNextItem", nil)
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTReadyNextItem command: %w", err)
	}

	for event := range eventChan {
		if event.IsComplete() {
			break
		}

		return fmt.Errorf("unexpected event")
	}

	return nil
}

func (c *Client) ListKeys() ([]string, error) {
	eventChan, invokeID, err := c.invokeCommand("AGTListKeys", nil)
	defer c.destroyCommand(invokeID)
	if err != nil {
		return nil, fmt.Errorf("error while executing AGTListKeys command: %w", err)
	}

	var dataSegments []string
	for event := range eventChan {
		if event.IsComplete() {
			continue
		}
		if event.Type == EventTypeData {
			dataSegments = append(dataSegments, event.Segments...)
			if event.Incomplete {
				continue
			}
			break
		}

		return nil, fmt.Errorf("unexpected event")
	}

	keys := make([]string, 0, len(dataSegments))
	for _, segment := range dataSegments {
		keys = append(keys, segment)
	}

	return keys, nil
}

func (c *Client) DetachJob() error {
	eventChan, invokeID, err := c.invokeCommand("AGTDetachJob", nil)
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTDetachJob command: %w", err)
	}

	for event := range eventChan {
		if event.IsComplete() {
			break
		}

		return fmt.Errorf("unexpected event")
	}

	return nil
}

func (c *Client) DisconnHeadset() error {
	eventChan, invokeID, err := c.invokeCommand("AGTDisconnHeadset", nil)
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTDisconnHeadset command: %w", err)
	}

	for event := range eventChan {
		if event.IsPending() {
			continue
		}
		if event.IsComplete() {
			break
		}

		return fmt.Errorf("unexpected event")
	}

	return nil
}

func (c *Client) FreeHeadset() error {
	eventChan, invokeID, err := c.invokeCommand("AGTFreeHeadset", nil)
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTFreeHeadset command: %w", err)
	}

	for event := range eventChan {
		if event.IsPending() {
			continue
		}
		if event.IsComplete() {
			break
		}

		return fmt.Errorf("unexpected event")
	}

	return nil
}

// Logoff sends ATGLogoff command, then Proactive Control server terminates session
func (c *Client) Logoff() error {
	eventChan, invokeID, err := c.invokeCommand("AGTLogoff", nil)
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTLogoff command: %w", err)
	}

	for event := range eventChan {
		if event.IsPending() {
			continue
		}
		if event.IsComplete() {
			break
		}

		return fmt.Errorf("unexpected event")
	}

	return nil
}
