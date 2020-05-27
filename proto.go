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
	RS rune = 0x1E
	// Incomplete message separator
	ETB rune = 0x17
	// End of text
	ETX rune = 0x03
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

type MessageCode string

const (
	MessageCodeComplete    MessageCode = "M00000"
	MessageCodeDataMessage MessageCode = "M00001"
	MessageCodePending     MessageCode = "S28833"
)

type MessageCodeType byte

const (
	MessageCodeTypeMessage MessageCodeType = 'M'
	MessageCodeTypeError   MessageCodeType = 'E'
	MessageCodeTypeStatus  MessageCodeType = 'S'
)

func (mc MessageCode) Type() MessageCodeType {
	if len(mc) < 1 {
		return 0
	}

	switch {
	case mc[0] == byte(MessageCodeTypeMessage):
		return MessageCodeTypeMessage
	case mc[0] == byte(MessageCodeTypeError):
		return MessageCodeTypeError
	case mc[0] == byte(MessageCodeTypeStatus):
		return MessageCodeTypeStatus
	}

	return 0
}

type Event struct {
	Keyword   string
	Type      EventType
	Client    string
	ProcessID uint32
	InvokeID  uint32
	Segments  []string

	CommandStatus uint8
	MessageCode   MessageCode

	Incomplete bool
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
		buf.WriteByte(byte(RS))
		for i, arg := range args {
			buf.WriteString(arg)

			if len(args)-1 != i {
				buf.WriteByte(byte(RS))
			}
		}
	}
	buf.WriteByte(byte(ETX))

	return buf.Bytes(), nil
}

func decodeEvent(atom string) (event Event, err error) {
	if len(atom) < 56 {
		return Event{}, errors.New("event len should be less or equal to 55 bytes")
	}

	event = Event{
		Keyword: strings.TrimSpace(atom[:20]),
		Type:    EventType(atom[20]),
		Client:  strings.TrimSpace(atom[21:41]),
	}

	processID, err := strconv.Atoi(strings.TrimSpace(atom[41:47]))
	if err != nil {
		return Event{}, err
	}
	event.ProcessID = uint32(processID)

	invokeID, err := strconv.Atoi(strings.TrimSpace(atom[47:51]))
	if err != nil {
		return Event{}, err
	}
	event.InvokeID = uint32(invokeID)

	numberOfSegments, err := strconv.Atoi(strings.TrimSpace(atom[51:55]))
	if err != nil {
		return Event{}, err
	}

	if numberOfSegments > 0 && len(atom) > 56 {
		segments := strings.Split(atom[56:], string(RS))
		for i, s := range segments {
			// Trim last byte if it reached the end
			if i == len(segments)-1 {
				if strings.HasSuffix(s, string(ETB)) {
					event.Incomplete = true
					s = strings.TrimSuffix(s, string(ETB))
				}

				if strings.HasSuffix(s, string(ETX)) {
					s = strings.TrimSuffix(s, string(ETX))
				}
			}

			// Read command status and message code
			switch i {
			case 0:
				t, err := strconv.Atoi(s)
				if err != nil {
					return Event{}, err
				}
				event.CommandStatus = uint8(t)
				continue
			case 1:
				event.MessageCode = MessageCode(s)
				continue
			}

			// Workaround: strip continue messages
			if j := strings.Index(s, string(ETB)); j != -1 {
				s = s[:j]
			}

			// Some responses may have windows-1251 encoded strings, so do some decoding
			/*s, err := charmap.Windows1251.NewDecoder().Bytes(s)
			if err != nil {
				return Event{}, err
			}*/

			event.Segments = append(event.Segments, s)
		}
	}

	return
}
