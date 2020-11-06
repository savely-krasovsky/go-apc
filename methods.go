package apc

import (
	"context"
	"fmt"
	"strconv"
	"strings"
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

func newRequest(ctx context.Context) *request {
	// Add cancellation context to parent one
	ctx, cancel := context.WithCancel(ctx)

	// Create dedicated event channel for this request
	return &request{
		context:   ctx,
		cancel:    cancel,
		eventChan: make(chan Event, 1),
	}
}

func (c *Client) invokeCommand(ctx context.Context, keyword string, args ...arg) (*request, uint32, error) {
	invokeID := c.invokeIDPool.Get()

	if c.state.Load() != ConnOK {
		return nil, invokeID, ErrConnectionClosed
	}

	fields := map[string]interface{}{
		"type":      string(EventTypeCommand),
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
	fields["segments"] = flatArgs

	// Encode command
	b, err := encodeCommand(keyword, invokeID, flatArgs...)
	if err != nil {
		return nil, invokeID, fmt.Errorf("cannot encode command: %w", err)
	}
	c.logger.log(newLogEntry(LogLevelDebug, "Command has encoded.", map[string]interface{}{"raw": string(b)}))

	// Write command to connection
	if _, err := c.conn.Write(b); err != nil {
		return nil, invokeID, fmt.Errorf("cannot write command: %w", err)
	}

	c.logger.log(newLogEntry(LogLevelInfo, "Command has sent.", fields))

	r := newRequest(ctx)

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

func (c *Client) Logon(ctx context.Context, agentName string, password string) error {
	r, invokeID, err := c.invokeCommand(ctx, "AGTLogon", newArg("agent_name", agentName), newArg("password", password), newArg("version", "GOLANG_0.0.3"))
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTLogon command: %w", err)
	}

	if _, err := processRequest(r); err != nil {
		return err
	}

	return nil
}

func (c *Client) ReserveHeadset(ctx context.Context, headsetID int) error {
	r, invokeID, err := c.invokeCommand(ctx, "AGTReserveHeadset", newArg("headset_id", strconv.Itoa(headsetID)))
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTReserveHeadset command: %w", err)
	}

	if _, err := processRequest(r); err != nil {
		return err
	}

	return nil
}

func (c *Client) ConnectHeadset(ctx context.Context) error {
	r, invokeID, err := c.invokeCommand(ctx, "AGTConnHeadset")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTConnHeadset command: %w", err)
	}

	if _, err := processRequest(r); err != nil {
		return err
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

func (c *Client) ListJobs(ctx context.Context, jobType JobType) ([]Job, error) {
	r, invokeID, err := c.invokeCommand(ctx, "AGTListJobs", newArg("job_type", string([]byte{byte(jobType)})))
	defer c.destroyCommand(invokeID)
	if err != nil {
		return nil, fmt.Errorf("error while executing AGTListJobs command: %w", err)
	}

	rawSegments, err := processRequest(r)
	if err != nil {
		return nil, err
	}

	jobs := make([]Job, 0, len(rawSegments))
	for _, segment := range rawSegments {
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

func (c *Client) ListCallLists(ctx context.Context) ([]string, error) {
	r, invokeID, err := c.invokeCommand(ctx, "AGTListCallLists")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return nil, fmt.Errorf("error while executing AGTListCallLists command: %w", err)
	}

	rawSegments, err := processRequest(r)
	if err != nil {
		return nil, err
	}

	callLists := make([]string, 0, len(rawSegments))
	for _, segment := range rawSegments {
		callLists = append(callLists, segment)
	}

	return callLists, nil
}

func (c *Client) ListCallFields(ctx context.Context, listName string) ([]string, error) {
	r, invokeID, err := c.invokeCommand(ctx, "AGTListCallFields", newArg("list_name", listName))
	defer c.destroyCommand(invokeID)
	if err != nil {
		return nil, fmt.Errorf("error while executing AGTListCallFields command: %w", err)
	}

	rawSegments, err := processRequest(r)
	if err != nil {
		return nil, err
	}

	callFields := make([]string, 0, len(rawSegments))
	for _, segment := range rawSegments {
		callFields = append(callFields, segment)
	}

	return callFields, nil
}

func (c *Client) AttachJob(ctx context.Context, jobName string) error {
	r, invokeID, err := c.invokeCommand(ctx, "AGTAttachJob", newArg("job_name", jobName))
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTAttachJob command: %w", err)
	}

	if _, err := processRequest(r); err != nil {
		return err
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

func (c *Client) ListDataFields(ctx context.Context, listType ListType) ([]DataField, error) {
	r, invokeID, err := c.invokeCommand(ctx, "AGTListDataFields", newArg("list_type", string([]byte{byte(listType)})))
	defer c.destroyCommand(invokeID)
	if err != nil {
		return nil, fmt.Errorf("error while executing AGTListDataFields command: %w", err)
	}

	rawSegments, err := processRequest(r)
	if err != nil {
		return nil, err
	}

	dataFields := make([]DataField, 0, len(rawSegments))
	for _, segment := range rawSegments {
		dataFieldParts := strings.Split(segment, ",")
		if len(dataFieldParts) == 4 {
			dataFields = append(dataFields, DataField{
				Name: dataFieldParts[0],
			})
		}
	}

	return dataFields, nil
}

func (c *Client) SetNotifyKeyField(ctx context.Context, listType ListType, fieldName string) error {
	r, invokeID, err := c.invokeCommand(ctx, "AGTSetNotifyKeyField", newArg("list_type", string([]byte{byte(listType)})), newArg("field_name", fieldName))
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTSetNotifyKeyField command: %w", err)
	}

	if _, err := processRequest(r); err != nil {
		return err
	}

	return nil
}

func (c *Client) SetDataField(ctx context.Context, listType ListType, fieldName string) error {
	r, invokeID, err := c.invokeCommand(ctx, "AGTSetDataField", newArg("list_type", string([]byte{byte(listType)})), newArg("field_name", fieldName))
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTSetDataField command: %w", err)
	}

	if _, err := processRequest(r); err != nil {
		return err
	}

	return nil
}

func (c *Client) AvailWork(ctx context.Context) error {
	r, invokeID, err := c.invokeCommand(ctx, "AGTAvailWork")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTAvailWork command: %w", err)
	}

	if _, err := processRequest(r); err != nil {
		return err
	}

	return nil
}

func (c *Client) ReadyNextItem(ctx context.Context) error {
	r, invokeID, err := c.invokeCommand(ctx, "AGTReadyNextItem")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTReadyNextItem command: %w", err)
	}

	if _, err := processRequest(r); err != nil {
		return err
	}

	return nil
}

func (c *Client) ListKeys(ctx context.Context) ([]string, error) {
	r, invokeID, err := c.invokeCommand(ctx, "AGTListKeys")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return nil, fmt.Errorf("error while executing AGTListKeys command: %w", err)
	}

	rawSegments, err := processRequest(r)
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(rawSegments))
	for _, segment := range rawSegments {
		keys = append(keys, segment)
	}

	return keys, nil
}

func (c *Client) ReleaseLine(ctx context.Context) error {
	r, invokeID, err := c.invokeCommand(ctx, "AGTReleaseLine")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTReleaseLine command: %w", err)
	}

	if _, err := processRequest(r); err != nil {
		return err
	}

	return nil
}

func (c *Client) FinishedItem(ctx context.Context, compCode int) error {
	r, invokeID, err := c.invokeCommand(ctx, "AGTFinishedItem", newArg("comp_code", strconv.Itoa(compCode)))
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTFinishedItem command: %w", err)
	}

	if _, err := processRequest(r); err != nil {
		return err
	}

	return nil
}

func (c *Client) NoFurtherWork(ctx context.Context) error {
	r, invokeID, err := c.invokeCommand(ctx, "AGTNoFurtherWork")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTNoFurtherWork command: %w", err)
	}

	if _, err := processRequest(r); err != nil {
		return err
	}

	return nil
}

func (c *Client) DetachJob(ctx context.Context) error {
	r, invokeID, err := c.invokeCommand(ctx, "AGTDetachJob")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTDetachJob command: %w", err)
	}

	if _, err := processRequest(r); err != nil {
		return err
	}

	return nil
}

func (c *Client) DisconnectHeadset(ctx context.Context) error {
	r, invokeID, err := c.invokeCommand(ctx, "AGTDisconnHeadset")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTDisconnHeadset command: %w", err)
	}

	if _, err := processRequest(r); err != nil {
		return err
	}

	return nil
}

func (c *Client) FreeHeadset(ctx context.Context) error {
	r, invokeID, err := c.invokeCommand(ctx, "AGTFreeHeadset")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTFreeHeadset command: %w", err)
	}

	if _, err := processRequest(r); err != nil {
		return err
	}

	return nil
}

// Logoff sends ATGLogoff command, then Proactive Control server terminates session
func (c *Client) Logoff(ctx context.Context) error {
	r, invokeID, err := c.invokeCommand(ctx, "AGTLogoff")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTLogoff command: %w", err)
	}

	if _, err := processRequest(r); err != nil {
		return err
	}

	return nil
}

func (c *Client) EchoOn(ctx context.Context) error {
	r, invokeID, err := c.invokeCommand(ctx, "AGTEchoOn")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTEchoOn command: %w", err)
	}

	if _, err := processRequest(r); err != nil {
		return err
	}

	return nil
}

func (c *Client) EchoOff(ctx context.Context) error {
	r, invokeID, err := c.invokeCommand(ctx, "AGTEchoOff")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTEchoOff command: %w", err)
	}

	if _, err := processRequest(r); err != nil {
		return err
	}

	return nil
}

func (c *Client) LogIoStart(ctx context.Context) error {
	r, invokeID, err := c.invokeCommand(ctx, "AGTLogIoStart")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTLogIoStart command: %w", err)
	}

	if _, err := processRequest(r); err != nil {
		return err
	}

	return nil
}

func (c *Client) LogIoStop(ctx context.Context) error {
	r, invokeID, err := c.invokeCommand(ctx, "AGTLogIoStop")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return fmt.Errorf("error while executing AGTLogIoStop command: %w", err)
	}

	if _, err := processRequest(r); err != nil {
		return err
	}

	return nil
}

type State struct {
	Type    StateType
	JobName string
}

type StateType string

const (
	StateTypeOnCall         StateType = "S70000"
	StateTypeReadyForCall   StateType = "S70001"
	StateTypeHasJoinedJob   StateType = "S70002"
	StateTypeHasSelectedJob StateType = "S70003"
	StateTypeLoggedOn       StateType = "S70004"
)

func (c *Client) ListState(ctx context.Context) (*State, error) {
	r, invokeID, err := c.invokeCommand(ctx, "AGTListState")
	defer c.destroyCommand(invokeID)
	if err != nil {
		return nil, fmt.Errorf("error while executing AGTListState command: %w", err)
	}

	rawSegments, err := processRequest(r)
	if err != nil {
		return nil, err
	}

	if rawSegments == nil || len(rawSegments) != 1 {
		return nil, fmt.Errorf("invalid segment")
	}

	parts := strings.Split(rawSegments[0], ",")
	if len(parts) != 1 && len(parts) != 2 {
		return nil, fmt.Errorf("invalid segment")
	}

	return &State{
		Type:    StateType(parts[0]),
		JobName: parts[1],
	}, nil
}

type Field struct {
	Name   string
	Type   FieldType
	Length int
	Value  string
}

type FieldType string

const (
	FieldTypeAlphanumeric FieldType = "A"
	FieldTypeNumeric      FieldType = "N"
	FieldTypeDate         FieldType = "D"
	FieldTypeCurrency     FieldType = "$"
	FieldTypeFutureUse    FieldType = "F"
)

func (c *Client) ReadField(ctx context.Context, listType ListType, fieldName string) (*Field, error) {
	r, invokeID, err := c.invokeCommand(ctx, "AGTReadField", newArg("list_type", string([]byte{byte(listType)})), newArg("field_name", fieldName))
	defer c.destroyCommand(invokeID)
	if err != nil {
		return nil, fmt.Errorf("error while executing AGTSetDataField command: %w", err)
	}

	rawSegments, err := processRequest(r)
	if err != nil {
		return nil, err
	}

	if rawSegments == nil || len(rawSegments) != 2 || rawSegments[0] == "M00001" {
		return nil, fmt.Errorf("invalid segment")
	}

	parts := strings.Split(rawSegments[1], ",")
	if len(parts) != 4 {
		return nil, fmt.Errorf("invalid segment")
	}

	length, err := strconv.Atoi(parts[2])
	if err != nil {
		return nil, fmt.Errorf("cannot convert field length: %w", err)
	}

	return &Field{
		Name:   parts[0],
		Type:   FieldType(parts[1]),
		Length: length,
		Value:  parts[3],
	}, nil
}
