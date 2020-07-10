package apc

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type arg struct {
	key   string
	value string
}

func newArg(key, value string) arg {
	return arg{
		key:   key,
		value: value,
	}
}

func (c *Client) invokeCommand(keyword string, args ...arg) (*request, uint32, error) {
	invokeID := c.invokeIDPool.Get()

	if c.state.Load() != ConnOK {
		return nil, invokeID, ErrConnectionClosed
	}

	fields := map[string]interface{}{
		"keyword":   keyword,
		"invoke_id": invokeID,
	}

	var flatArgs []string
	if len(args) > 0 {
		flatArgs = make([]string, 0, len(args))
		for _, arg := range args {
			flatArgs = append(flatArgs, arg.value)
			fields[arg.key] = arg.value
		}
	}

	// Encode command
	b, err := encodeCommand(keyword, invokeID, flatArgs...)
	if err != nil {
		return nil, invokeID, fmt.Errorf("cannot encode command: %w", err)
	}
	c.logger.log(newLogEntry(LogLevelDebug, "Command has encoded.", map[string]interface{}{"raw": b}))

	// Write command to connection
	if _, err := c.conn.Write(b); err != nil {
		return nil, invokeID, fmt.Errorf("cannot write command: %w", err)
	}

	c.logger.log(newLogEntry(LogLevelInfo, "Command has sent.", fields))

	// Create dedicated event channel for this request
	r := &request{
		eventChan: make(chan Event, 1),
		done:      make(chan struct{}, 1),
	}

	c.mu.Lock()
	c.requests[invokeID] = r
	c.mu.Unlock()

	return r, invokeID, nil
}

func (c *Client) destroyCommand(invokeID uint32) {
	c.mu.RLock()
	_, ok := c.requests[invokeID]
	c.mu.RUnlock()

	// in case of executeCommand func returned an error just release invoke id from pool
	if !ok {
		c.invokeIDPool.Release(invokeID)
		return
	}

	// Delete request from pool
	c.mu.Lock()
	delete(c.requests, invokeID)
	c.mu.Unlock()

	// Finally release invoke ID
	c.invokeIDPool.Release(invokeID)
}

func (c *Client) Logon(agentName string, password string) error {
	r, invokeID, err := c.invokeCommand("AGTLogon", newArg("agent_name", agentName), newArg("password", password), newArg("version", "GOLANG_0.0.2"))
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTLogon command: %w", err)
	}

el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsPending() {
				continue
			}
			if event.IsComplete() {
				break el
			}

			return fmt.Errorf("unexpected event")
		case <-r.done:
			return context.Canceled
		}
	}

	return nil
}

func (c *Client) ReserveHeadset(headsetID int) error {
	r, invokeID, err := c.invokeCommand("AGTReserveHeadset", newArg("headset_id", strconv.Itoa(headsetID)))
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTReserveHeadset command: %w", err)
	}

el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsPending() {
				continue
			}
			if event.IsComplete() {
				break el
			}

			return fmt.Errorf("unexpected event")
		case <-r.done:
			return context.Canceled
		}
	}

	return nil
}

func (c *Client) ConnectHeadset() error {
	r, invokeID, err := c.invokeCommand("AGTConnHeadset")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTConnHeadset command: %w", err)
	}

el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsPending() {
				continue
			}
			if event.IsComplete() {
				break el
			}

			return fmt.Errorf("unexpected event")
		case <-r.done:
			return context.Canceled
		}
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
	r, invokeID, err := c.invokeCommand("AGTListJobs", newArg("job_type", string([]byte{byte(jobType)})))
	defer c.destroyCommand(invokeID)
	if err != nil {
		return nil, fmt.Errorf("error while executing AGTListJobs command: %w", err)
	}

	var dataSegments []string
el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsComplete() {
				continue
			}
			if event.Type == EventTypeData {
				dataSegments = append(dataSegments, event.Segments...)
				if event.Incomplete {
					continue
				}
				break el
			}

			return nil, fmt.Errorf("unexpected event")
		case <-r.done:
			return nil, context.Canceled
		}
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
	r, invokeID, err := c.invokeCommand("AGTListCallLists")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return nil, fmt.Errorf("error while executing AGTListCallLists command: %w", err)
	}

	var dataSegments []string
el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsComplete() {
				continue
			}
			if event.Type == EventTypeData {
				dataSegments = append(dataSegments, event.Segments...)
				if event.Incomplete {
					continue
				}
				break el
			}

			return nil, fmt.Errorf("unexpected event")
		case <-r.done:
			return nil, context.Canceled
		}
	}

	callLists := make([]string, 0, len(dataSegments))
	for _, segment := range dataSegments {
		callLists = append(callLists, segment)
	}

	return callLists, nil
}

func (c *Client) ListCallFields(listName string) ([]string, error) {
	r, invokeID, err := c.invokeCommand("AGTListCallFields", newArg("list_name", listName))
	defer c.destroyCommand(invokeID)
	if err != nil {
		return nil, fmt.Errorf("error while executing AGTListCallFields command: %w", err)
	}

	var dataSegments []string
el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsComplete() {
				continue
			}
			if event.Type == EventTypeData {
				dataSegments = append(dataSegments, event.Segments...)
				if event.Incomplete {
					continue
				}
				break el
			}

			return nil, fmt.Errorf("unexpected event")
		case <-r.done:
			return nil, context.Canceled
		}
	}

	callFields := make([]string, 0, len(dataSegments))
	for _, segment := range dataSegments {
		callFields = append(callFields, segment)
	}

	return callFields, nil
}

func (c *Client) AttachJob(jobName string) error {
	r, invokeID, err := c.invokeCommand("AGTAttachJob", newArg("job_name", jobName))
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTAttachJob command: %w", err)
	}

el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsPending() {
				continue
			}
			if event.IsComplete() {
				break el
			}

			return fmt.Errorf("unexpected event")
		case <-r.done:
			return context.Canceled
		}
	}

	return nil
}

type ListType byte

const (
	ListTypeOutbound ListType = 'O'
	ListTypeInbound  ListType = 'I'
)

type DataField struct {
	Name string
}

func (c *Client) ListDataFields(listType ListType) ([]DataField, error) {
	r, invokeID, err := c.invokeCommand("AGTListDataFields", newArg("list_type", string([]byte{byte(listType)})))
	defer c.destroyCommand(invokeID)
	if err != nil {
		return nil, fmt.Errorf("error while executing AGTListDataFields command: %w", err)
	}

	var dataSegments []string
el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsComplete() {
				continue
			}
			if event.Type == EventTypeData {
				dataSegments = append(dataSegments, event.Segments...)
				if event.Incomplete {
					continue
				}
				break el
			}

			return nil, fmt.Errorf("unexpected event")
		case <-r.done:
			return nil, context.Canceled
		}
	}

	dataFields := make([]DataField, 0, len(dataSegments))
	for _, segment := range dataSegments {
		dataFieldParts := strings.Split(segment, ",")
		if len(dataFieldParts) == 4 {
			dataFields = append(dataFields, DataField{
				Name: dataFieldParts[0],
			})
		}
	}

	return dataFields, nil
}

func (c *Client) SetNotifyKeyField(listType ListType, fieldName string) error {
	r, invokeID, err := c.invokeCommand("AGTSetNotifyKeyField", newArg("list_type", string([]byte{byte(listType)})), newArg("field_name", fieldName))
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTSetNotifyKeyField command: %w", err)
	}

el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsComplete() {
				break el
			}

			return fmt.Errorf("unexpected event")
		case <-r.done:
			return context.Canceled
		}
	}

	return nil
}

func (c *Client) SetDataField(listType ListType, fieldName string) error {
	r, invokeID, err := c.invokeCommand("AGTSetDataField", newArg("list_type", string([]byte{byte(listType)})), newArg("field_name", fieldName))
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTSetDataField command: %w", err)
	}

el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsComplete() {
				break el
			}

			return fmt.Errorf("unexpected event")
		case <-r.done:
			return context.Canceled
		}
	}

	return nil
}

func (c *Client) AvailWork() error {
	r, invokeID, err := c.invokeCommand("AGTAvailWork")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTAvailWork command: %w", err)
	}

el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsPending() {
				continue
			}
			if event.IsComplete() {
				break el
			}

			return fmt.Errorf("unexpected event")
		case <-r.done:
			return context.Canceled
		}
	}

	return nil
}

func (c *Client) ReadyNextItem() error {
	r, invokeID, err := c.invokeCommand("AGTReadyNextItem")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTReadyNextItem command: %w", err)
	}

el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsComplete() {
				break el
			}

			return fmt.Errorf("unexpected event")
		case <-r.done:
			return context.Canceled
		}
	}

	return nil
}

func (c *Client) ListKeys() ([]string, error) {
	r, invokeID, err := c.invokeCommand("AGTListKeys")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return nil, fmt.Errorf("error while executing AGTListKeys command: %w", err)
	}

	var dataSegments []string
el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsComplete() {
				continue
			}
			if event.Type == EventTypeData {
				dataSegments = append(dataSegments, event.Segments...)
				if event.Incomplete {
					continue
				}
				break el
			}

			return nil, fmt.Errorf("unexpected event")
		case <-r.done:
			return nil, context.Canceled
		}
	}

	keys := make([]string, 0, len(dataSegments))
	for _, segment := range dataSegments {
		keys = append(keys, segment)
	}

	return keys, nil
}

func (c *Client) ReleaseLine() error {
	r, invokeID, err := c.invokeCommand("AGTReleaseLine")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTReleaseLine command: %w", err)
	}

el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsPending() {
				continue
			}
			if event.IsComplete() {
				break el
			}

			return fmt.Errorf("unexpected event")
		case <-r.done:
			return context.Canceled
		}
	}

	return nil
}

func (c *Client) FinishedItem(compCode int) error {
	r, invokeID, err := c.invokeCommand("AGTFinishedItem", newArg("comp_code", strconv.Itoa(compCode)))
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTFinishedItem command: %w", err)
	}

el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsPending() {
				continue
			}
			if event.IsComplete() {
				break el
			}

			return fmt.Errorf("unexpected event")
		case <-r.done:
			return context.Canceled
		}
	}

	return nil
}

func (c *Client) NoFurtherWork() error {
	r, invokeID, err := c.invokeCommand("AGTNoFurtherWork")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTNoFurtherWork command: %w", err)
	}

el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsPending() {
				continue
			}
			if event.IsComplete() {
				break el
			}

			return fmt.Errorf("unexpected event")
		// TODO: TEMPORARY WORKAROUND
		case <-time.After(30 * time.Second):
			return context.DeadlineExceeded
		case <-r.done:
			return context.Canceled
		}
	}

	return nil
}

func (c *Client) DetachJob() error {
	r, invokeID, err := c.invokeCommand("AGTDetachJob")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTDetachJob command: %w", err)
	}

el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsComplete() {
				break el
			}

			return fmt.Errorf("unexpected event")
		case <-r.done:
			return context.Canceled
		}
	}

	return nil
}

func (c *Client) DisconnectHeadset() error {
	r, invokeID, err := c.invokeCommand("AGTDisconnHeadset")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTDisconnHeadset command: %w", err)
	}

el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsPending() {
				continue
			}
			if event.IsComplete() {
				break el
			}

			return fmt.Errorf("unexpected event")
		case <-r.done:
			return context.Canceled
		}
	}

	return nil
}

func (c *Client) FreeHeadset() error {
	r, invokeID, err := c.invokeCommand("AGTFreeHeadset")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTFreeHeadset command: %w", err)
	}

el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsPending() {
				continue
			}
			if event.IsComplete() {
				break el
			}

			return fmt.Errorf("unexpected event")
		case <-r.done:
			return context.Canceled
		}
	}

	return nil
}

// Logoff sends ATGLogoff command, then Proactive Control server terminates session
func (c *Client) Logoff() error {
	r, invokeID, err := c.invokeCommand("AGTLogoff")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTLogoff command: %w", err)
	}

el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsPending() {
				continue
			}
			if event.IsComplete() {
				break el
			}

			return fmt.Errorf("unexpected event")
		case <-r.done:
			return context.Canceled
		}
	}

	return nil
}

func (c *Client) EchoOn() error {
	r, invokeID, err := c.invokeCommand("AGTEchoOn")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTEchoOn command: %w", err)
	}

el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsComplete() {
				break el
			}

			return fmt.Errorf("unexpected event")
		case <-r.done:
			return context.Canceled
		}
	}

	return nil
}

func (c *Client) EchoOff() error {
	r, invokeID, err := c.invokeCommand("AGTEchoOff")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTEchoOff command: %w", err)
	}

el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsComplete() {
				break el
			}

			return fmt.Errorf("unexpected event")
		case <-r.done:
			return context.Canceled
		}
	}

	return nil
}

func (c *Client) LogIoStart() error {
	r, invokeID, err := c.invokeCommand("AGTLogIoStart")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTLogIoStart command: %w", err)
	}

el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsComplete() {
				break el
			}

			return fmt.Errorf("unexpected event")
		case <-r.done:
			return context.Canceled
		}
	}

	return nil
}

func (c *Client) LogIoStop() error {
	r, invokeID, err := c.invokeCommand("AGTLogIoStop")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTLogIoStop command: %w", err)
	}

el:
	for {
		select {
		case event := <-r.eventChan:
			if event.IsComplete() {
				break el
			}

			return fmt.Errorf("unexpected event")
		case <-r.done:
			return context.Canceled
		}
	}

	return nil
}

type State string

const (
	StateOnCall         State = "S70000"
	StateReadyForCall   State = "S70001"
	StateHasJoinedJob   State = "S70002"
	StateHasSelectedJob State = "S70003"
	StateLoggedOn       State = "S70004"
)

func (c *Client) ListState() (State, error) {
	r, invokeID, err := c.invokeCommand("AGTListState")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return "", fmt.Errorf("error while executing AGTListState command: %w", err)
	}

	var state State

el:
	for {
		select {
		case event := <-r.eventChan:
			if len(event.Segments) == 2 &&
				event.Segments[0] == "0" &&
				strings.HasPrefix(event.Segments[1], "S") {
				if strings.HasPrefix(event.Segments[1], "S70004") {
					state = State(event.Segments[1])
					continue
				}

				parts := strings.Split(event.Segments[1], ",")
				state = State(parts[0])
				continue
			}

			if event.IsComplete() {
				break el
			}

			return "", fmt.Errorf("unexpected event")
		case <-r.done:
			return "", context.Canceled
		}
	}

	return state, nil
}
