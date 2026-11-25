//go:build !windows

package daemon

// IsRocketLeagueRunning reports whether Rocket League is detected as a local OS process.
func IsRocketLeagueRunning() bool {
	return false
}
