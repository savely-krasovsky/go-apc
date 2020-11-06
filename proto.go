package apc

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	// Message separator
	RS byte = 0x1E
	// IsIncomplete message separator
	ETB byte = 0x17
	// End of text
	ETX byte = 0x03
)

type EventType byte

const (
	EventTypeCommand      EventType = 'C'
	EventTypePending      EventType = 'P'
	EventTypeData         EventType = 'D'
	EventTypeResponse     EventType = 'R'
	EventTypeBusy         EventType = 'B'
	EventTypeNotification EventType = 'N'
)

type Event struct {
	Keyword      string
	Type         EventType
	Client       string
	ProcessID    uint32
	InvokeID     uint32
	Segments     []string
	IsIncomplete bool
}

func (e Event) IsStart() bool {
	if e.Type != EventTypeNotification ||
		len(e.Segments) < 2 ||
		e.Segments[0] != "0" ||
		e.Segments[1] != "AGENT_STARTUP" {
		return false
	}

	return true
}

func (e Event) IsPending() bool {
	if e.Type != EventTypePending ||
		len(e.Segments) < 2 ||
		e.Segments[0] != "0" ||
		e.Segments[1] != "S28833" {
		return false
	}

	return true
}

func (e Event) IsDataMessage() bool {
	if e.Type != EventTypeData ||
		len(e.Segments) < 2 ||
		e.Segments[0] != "0" {
		return false
	}

	return true
}

func (e Event) IsSuccessfulResponse() bool {
	if e.Type != EventTypeResponse ||
		len(e.Segments) < 2 ||
		e.Segments[0] != "0" ||
		e.Segments[1] != "M00000" {
		return false
	}

	return true
}

func (e Event) IsResponseError() bool {
	if e.Type != EventTypeResponse ||
		len(e.Segments) < 2 ||
		e.Segments[0] != "1" ||
		len(e.Segments[1]) != 6 ||
		!strings.HasPrefix(e.Segments[1], "E") {
		return false
	}

	return true
}

func (e Event) IsSuccessfulNotification() bool {
	if e.Type != EventTypeNotification ||
		len(e.Segments) < 2 ||
		e.Segments[0] != "0" ||
		e.Segments[1] != "M00000" {
		return false
	}

	return true
}

func (e Event) IsNotificationError() bool {
	if e.Type != EventTypeNotification ||
		len(e.Segments) < 2 ||
		e.Segments[0] != "1" ||
		len(e.Segments[1]) != 6 ||
		!strings.HasPrefix(e.Segments[1], "E") {
		return false
	}

	return true
}

func (e Event) IsNotificationData() bool {
	if e.Type != EventTypeNotification ||
		len(e.Segments) < 2 ||
		e.Segments[0] != "0" ||
		e.Segments[1] != "M00001" {
		return false
	}

	return true
}

func encodeCommand(keyword string, invokeID uint32, args ...string) ([]byte, error) {
	// Checks
	if len(keyword) > 20 {
		return nil, errors.New("keyword should be less or equal to 20 bytes")
	}
	if len(strconv.Itoa(int(invokeID))) > 4 {
		return nil, errors.New("invoke id should be less or equal to 4 bytes")
	}

	buf := bytes.NewBuffer(nil)

	// Keyword; 20 bytes
	buf.WriteString(fmt.Sprintf("%-20s", keyword))

	// Type; 1 byte
	buf.WriteByte('C')

	// Client; 20 bytes
	buf.WriteString(fmt.Sprintf("%-20s", "Golang"))

	// Process ID; 6 bytes
	buf.WriteString(fmt.Sprintf("%-6d", 0))

	// Invoke ID; 4 bytes
	buf.WriteString(fmt.Sprintf("%-4d", invokeID))

	// Number of segments; 4 bytes
	buf.WriteString(fmt.Sprintf("%-4d", len(args)))

	if len(args) > 0 {
		buf.WriteByte(RS)
		for i, arg := range args {
			buf.WriteString(arg)

			if len(args)-1 != i {
				buf.WriteByte(RS)
			}
		}
	}
	buf.WriteByte(ETX)

	return buf.Bytes(), nil
}

type decodingError struct {
	error
}

func newDecodingError(text string) error {
	return &decodingError{errors.New(text)}
}

func IsDecodingError(err error) bool {
	_, ok := err.(*decodingError)
	return ok
}

func decodeEvent(raw string) (event Event, err error) {
	if len(raw) < 56 {
		return Event{}, newDecodingError("event len should be less or equal to 55 bytes")
	}

	event = Event{
		Keyword: strings.TrimSpace(raw[:20]),
		Type:    EventType(raw[20]),
		Client:  strings.TrimSpace(raw[21:41]),
	}

	processID, err := strconv.Atoi(strings.TrimSpace(raw[41:47]))
	if err != nil {
		return Event{}, newDecodingError("cannot parse process id as int")
	}
	event.ProcessID = uint32(processID)

	invokeID, err := strconv.Atoi(strings.TrimSpace(raw[47:51]))
	if err != nil {
		return Event{}, newDecodingError("cannot parse invoke id as int")
	}
	event.InvokeID = uint32(invokeID)

	numberOfSegments, err := strconv.Atoi(strings.TrimSpace(raw[51:55]))
	if err != nil {
		return Event{}, newDecodingError("cannot parse number of segments as int")
	}

	if numberOfSegments > 0 && len(raw) > 56 {
		segments := strings.Split(raw[56:], string(RS))
		for i, s := range segments {
			// Trim last byte if it reached the end
			if i == len(segments)-1 {
				if strings.HasSuffix(s, string(ETB)) {
					event.IsIncomplete = true
					s = strings.TrimSuffix(s, string(ETB))
				}

				if strings.HasSuffix(s, string(ETX)) {
					s = strings.TrimSuffix(s, string(ETX))
				}
			}

			event.Segments = append(event.Segments, s)
		}
	}

	return
}

type AvayaError struct {
	Code string
}

func (e AvayaError) Error() string {
	return e.Code
}

func processRequest(r *request) ([]string, error) {
	var (
		dataSegments []string
		batch        bool
	)
el:
	for {
		select {
		case event := <-r.eventChan:
			switch {
			// Skip pending events
			case event.IsPending():
				continue
			// Handle data messages and wait successful request
			case event.IsDataMessage():
				dataSegments = append(dataSegments, event.Segments[1:]...)
				// If event is incomplete then mark it as a batch
				if event.IsIncomplete {
					batch = true
				}
				continue
			case batch:
				dataSegments = append(dataSegments, event.Segments...)
				// If event is complete then unmark it as a batch
				if !event.IsIncomplete {
					batch = false
				}
				continue
			// Break the loop in case of success
			case event.IsSuccessfulResponse():
				break el
			// Return error immediately
			case event.IsResponseError():
				return nil, AvayaError{Code: event.Segments[1]}
			default:
				return nil, fmt.Errorf("unexpected event")
			}
		case <-r.context.Done():
			return nil, r.context.Err()
		}
	}

	return dataSegments, nil
}

type Notification struct {
	Type    NotificationType
	Payload interface{}
}

type NotificationType string

const (
	NotificationTypeCallNotify        NotificationType = "AGTCallNotify"
	NotificationTypeAutoReleaseLine   NotificationType = "AGTAutoReleaseLine"
	NotificationTypeJobEnd            NotificationType = "AGTJobEnd"
	NotificationTypeReceiveMessage    NotificationType = "AGTReceiveMessage"
	NotificationTypeJobTransRequest   NotificationType = "AGTJobTransRequest"
	NotificationTypeHeadsetConnBroken NotificationType = "AGTHeadsetConnBroken"
	NotificationTypeSystemError       NotificationType = "AGTSystemError"
)

func processNotifications(r *request, notifications chan<- Notification) {
	var (
		state   int
		fields  map[string]string
		message string
		jobName string
	)

	for {
		select {
		case event := <-r.eventChan:
			switch {
			case event.IsNotificationData():
				switch NotificationType(event.Keyword) {
				case NotificationTypeCallNotify:
					switch state {
					case 0:
						state++
					case 1:
						for _, s := range event.Segments[2:] {
							fields = make(map[string]string)
							parts := strings.Split(s, ",")
							if len(parts) != 2 {
								continue
							}

							fields[parts[0]] = parts[1]
						}
						state++
					}
				case NotificationTypeReceiveMessage:
					message = event.Segments[2]
				case NotificationTypeJobTransRequest:
					jobName = event.Segments[2]
				}
			case event.IsSuccessfulNotification():
				n := Notification{Type: NotificationType(event.Keyword)}

				switch n.Type {
				case NotificationTypeCallNotify:
					n.Payload = fields
					state = 0
					fields = nil
				case NotificationTypeReceiveMessage:
					n.Payload = message
					message = ""
				case NotificationTypeJobTransRequest:
					n.Payload = jobName
					jobName = ""
				}

				notifications <- n
			case event.IsNotificationError():
				notifications <- Notification{Type: NotificationType(event.Keyword), Payload: event.Segments[1]}
			}
		case <-r.context.Done():
			return
		}
	}
}
