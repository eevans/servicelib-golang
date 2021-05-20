/*
 * Copyright 2019 Clara Andrew-Wani <candrew@wikimedia.org>, Eric Evans <eevans@wikimedia.org>,
 * and Wikimedia Foundation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

type Level int

const (
	// Log levels
	DEBUG = iota
	INFO
	WARNING
	ERROR
	FATAL
)

// Logger formats and delivers log messages (see: NewLogger()).
type Logger struct {
	writer      io.Writer
	serviceName string
	serviceType string
	logLevel    Level
}

type ecsClient struct {
	Bytes int    `json:"bytes,omitempty"`
	IP    string `json:"ip,omitempty"`
	Port  string `json:"port,omitempty"`
}

type ecsLog struct {
	Level string `json:"level"`
}

type ecsNetwork struct {
	ForwardedIP string `json:"forwarded_ip,omitempty"`
}

type ecsService struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
}

type ecsTrace struct {
	ID string `json:"id,omitempty"`
}

// LogMessage represents JSON serializable log messages.
type LogMessage struct {
	Timestamp string      `json:"@timestamp"`
	Message   string      `json:"message"`
	Client    *ecsClient  `json:"client,omitempty"`
	Log       ecsLog      `json:"log"`
	Network   *ecsNetwork `json:"network,omitempty"`
	Service   ecsService  `json:"service"`
	Trace     *ecsTrace   `json:"trace,omitempty"`
}

// RequestScopedLogger formats and delivers a Logger and optional LogMessage attributes.
type RequestScopedLogger struct {
	logger  *Logger
	client  *ecsClient
	network *ecsNetwork
	trace   *ecsTrace
}

// Log creates a LogMessage at the specified level.
func (s *RequestScopedLogger) Log(level Level, format string, v ...interface{}) {
	s.logger.log(level, func() LogMessage {
		return LogMessage{
			Timestamp: time.Now().Format(time.RFC3339),
			Message:   fmt.Sprintf(format, v...),
			Client:    s.client,
			Log:       ecsLog{Level: LevelString(level)},
			Service:   ecsService{Name: s.logger.serviceName, Type: s.logger.serviceType},
			Trace:     s.trace,
		}
	})
}

// ClientBytes sets a client bytes attribute on subsequent log messages
func (s *RequestScopedLogger) ClientBytes(size int) *RequestScopedLogger {
	if s.client == nil {
		s.client = &ecsClient{}
	}
	s.client.Bytes = size
	return s
}

// ClientIP sets a client IP attribute on subsequent log messages.
func (s *RequestScopedLogger) ClientIP(ip string) *RequestScopedLogger {
	if s.client == nil {
		s.client = &ecsClient{}
	}
	s.client.IP = ip
	return s
}

// ClientPort sets a client port attribute on subsequent log messages.
func (s *RequestScopedLogger) ClientPort(port string) *RequestScopedLogger {
	if s.client == nil {
		s.client = &ecsClient{}
	}
	s.client.Port = port
	return s
}

// NetworkForwardedIP sets an attribute for the forwarded IP address on subsequent log messages.
func (s *RequestScopedLogger) NetworkForwardedIP(ip string) *RequestScopedLogger {
	if s.network == nil {
		s.network = &ecsNetwork{}
	}
	s.network.ForwardedIP = ip
	return s
}

// Trace sets a trace ID attribute on subsequent log messages.
func (s *RequestScopedLogger) Trace(id string) *RequestScopedLogger {
	if s.trace == nil {
		s.trace = &ecsTrace{}
	}
	s.trace.ID = id
	return s
}

// Request creates and returns a request-scoped Logger
func (l *Logger) Request() *RequestScopedLogger {
	return &RequestScopedLogger{logger: l}
}

// This is an internal implementation; The application should log messages
// using one of the level-specific methods, or a ScopedLogger as appropriate.
// Note: This method accepts a function that returns a LogMessage struct,
// instead of directly accepting a LogMessage, so that any costly string
// formatting can occur only if the message will be logged.
func (l *Logger) log(level Level, msg func() LogMessage) {
	// Level must be one of the constants declared above; We do not allow ad hoc logging levels.
	if !validLevel(level) {
		l.Error("Invalid log level specified (%d); This is a bug!", level)
		level = ERROR
	}

	// Skip if level is below what we're configured to log.
	if level < l.logLevel {
		return
	}

	message := msg()

	str, err := json.Marshal(message)

	// Handle the (unlikely) case where JSON serialization fails.
	if err != nil {
		l.send(fmt.Sprintf(`{"message": "Error serializing log message: %v (%s)", "service": {"name": "%s"}}`, message, err, l.serviceName))
		return
	}

	// Log the messsage to the underlying io.Writer, one message per line.
	l.send(string(str))
}

// Fatal logs messages of severity FATAL.
func (l *Logger) Fatal(format string, v ...interface{}) {
	l.log(FATAL, l.basicLogMessage(FATAL, format, v...))
}

// Error logs messages of severity ERROR.
func (l *Logger) Error(format string, v ...interface{}) {
	l.log(ERROR, l.basicLogMessage(ERROR, format, v...))
}

// Warning logs messages of severity WARNING.
func (l *Logger) Warning(format string, v ...interface{}) {
	l.log(WARNING, l.basicLogMessage(WARNING, format, v...))
}

// Info logs messages of severity INFO.
func (l *Logger) Info(format string, v ...interface{}) {
	l.log(INFO, l.basicLogMessage(INFO, format, v...))
}

// Debug logs messages of severity DEBUG.
func (l *Logger) Debug(format string, v ...interface{}) {
	l.log(DEBUG, l.basicLogMessage(DEBUG, format, v...))
}

// Write logs messages of severity WARNING.  This method satisfies the io.Writer
// interface so that Logger instances can be used as output for Golang's log module.
func (l *Logger) Write(bytes []byte) (int, error) {
	l.log(WARNING, l.basicLogMessage(WARNING, strings.TrimSuffix(string(bytes), "\n")))
	return len(bytes), nil
}

func (l *Logger) send(s string) {
	// TODO: Should error handling be added to this? Our io.Writer will likely always be
	// os.Stdout, what would we do if unable to write to stdout?
	fmt.Fprintln(l.writer, s)
}

// This is an (internal) utility method for creating simple LogMessage (functions).
func (l *Logger) basicLogMessage(level Level, format string, v ...interface{}) func() LogMessage {
	return func() LogMessage {
		return LogMessage{
			Message:   fmt.Sprintf(format, v...),
			Timestamp: time.Now().Format(time.RFC3339),
			Service:   ecsService{Name: l.serviceName, Type: l.serviceType},
			Log:       ecsLog{Level: LevelString(level)},
		}
	}
}

func validLevel(level Level) bool {
	switch level {
	case DEBUG, INFO, WARNING, ERROR, FATAL:
		return true
	}
	return false
}

// LevelString converts log integers to strings
func LevelString(level Level) string {
	switch level {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARNING:
		return "WARNING"
	case ERROR:
		return "ERROR"
	case FATAL:
		return "FATAL"
	default:
		return ""
	}
}

// NewLogger creates a new Logger instance using arguments for an io.Writer, service name and type, and
// a log level.
//
// The serviceName and serviceType arguments correspond to service.name and service.type in the ECS
// specification (see: https://doc.wikimedia.org/ecs/#ecs-service); Broadly speaking, service type refers
// to the software system implementing logging, and service name to the instance of that system sending
// log data.
//
// The logLevel argument must be set to one of DEBUG, INFO, WARNING, ERROR, or FATAL.  Only messages
// logged at this level - or higher - are formatted and output.
func NewLogger(writer io.Writer, serviceName, serviceType string, logLevel Level) (*Logger, error) {

	if !validLevel(logLevel) {
		return nil, fmt.Errorf("Unsupported log level: %d", logLevel)
	}

	return &Logger{writer, serviceName, serviceType, logLevel}, nil
}
