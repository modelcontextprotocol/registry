package appinfo

import "time"

// Application start time
var startTime = time.Now()
var Version = "v0.1.0"

// Initialize sets the application start time
func Initialize() {
	startTime = time.Now()
}

// GetUptime returns the time the application has been running
func GetUptime() time.Duration {
	return time.Since(startTime).Truncate(time.Second)
}

// GetUptimeString returns a formatted uptime string, e.g. "2h30m15s"
func GetUptimeString() string {
	return GetUptime().String()
}
