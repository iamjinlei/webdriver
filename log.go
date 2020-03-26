// Package log provides logging-related configuration types and constants.
package webdriver

import "time"

// Type represents a component capable of logging.
type LogType string

// The valid log types.
const (
	Server      LogType = "server"
	Browser     LogType = "browser"
	Client      LogType = "client"
	Driver      LogType = "driver"
	Performance LogType = "performance"
	Profiler    LogType = "profiler"
)

// Level represents a logging level of different components in the browser,
// the driver, or any intermediary WebDriver servers.
//
// See the documentation of each driver for what browser specific logging
// components are available.
type LogLevel string

// The valid log levels.
const (
	Off     LogLevel = "OFF"
	Severe  LogLevel = "SEVERE"
	Warning LogLevel = "WARNING"
	Info    LogLevel = "INFO"
	Debug   LogLevel = "DEBUG"
	All     LogLevel = "ALL"
)

// CapabilitiesKey is the key for the logging preferences entry in the JSON
// structure representing WebDriver capabilities.
//
// Note that the W3C spec does not include logging right now, and starting with
// Chrome 75, "loggingPrefs" has been changed to "goog:loggingPrefs"
const LogCapabilitiesKey = "goog:loggingPrefs"

// Capabilities is the map to include in the WebDriver capabilities structure
// to configure logging.
type LogCapabilities map[LogType]LogLevel

// Message is a log message returned from the Log method.
type Message struct {
	Timestamp time.Time
	Level     LogLevel
	Message   string
}
