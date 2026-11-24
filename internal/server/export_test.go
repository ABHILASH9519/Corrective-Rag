package server

import "time"

// SetHeartbeat changes the keep-alive interval for a test and returns a function that restores it.
func SetHeartbeat(d time.Duration) (restore func()) {
	old := heartbeat
	heartbeat = d
	return func() { heartbeat = old }
}
