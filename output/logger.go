//go:build linux || darwin || windows
// +build linux darwin windows

package main

import "log"

// Logger is a simple local logger.
type Logger struct {
	debug  bool
	prefix string
}

// NewLogger is the constructor for the logger class.
func NewLogger(prefix string, debug bool) *Logger {
	return &Logger{
		debug:  debug,
		prefix: prefix,
	}
}

// Debug prints logs only if debug flag is true.
func (l *Logger) Debug(message string) {
	if l.debug {
		log.Printf("[%s] %s", l.prefix, message)
	}
}

// Log prints every log.
func (l *Logger) Log(message string) {
	log.Printf("[%s] %s", l.prefix, message)
}

// SetDebug sets debug flag.
func (l *Logger) SetDebug(debug bool) {
	l.debug = debug
}
