package statsapi

import (
	"encoding/json"
	"time"
)

// EventMessage represents a JSON message broadcast by Rocket League's MatchStatsExporter_TA.
type EventMessage struct {
	Event string    `json:"Event"`
	Data  EventData `json:"Data"`
}

// EventData contains event-specific payload fields broadcast by the Rocket League Stats API.
type EventData struct {
	MatchGuid string          `json:"MatchGuid,omitempty"`
	Raw       json.RawMessage `json:"-"`
}

// UnmarshalJSON unmarshals event data while tolerating varied payloads.
func (ed *EventData) UnmarshalJSON(data []byte) error {
	type Alias EventData
	var a Alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*ed = EventData(a)
	ed.Raw = append(ed.Raw[:0], data...)
	return nil
}

// PendingMatch tracks an online match detected via Stats API that has not yet been downloaded/persisted.
type PendingMatch struct {
	MatchGUID  string    `json:"match_guid"`
	DetectedAt time.Time `json:"detected_at"`
}

// Status represents the current operational status of the Stats API tracking module.
type Status struct {
	Enabled            bool           `json:"enabled"`
	GameConnected      bool           `json:"game_connected"`
	PendingCount       int            `json:"pending_count"`
	TriggerThreshold   int            `json:"trigger_threshold"`
	ForceSyncOnTrigger bool           `json:"force_sync_on_trigger"`
	PendingMatches     []PendingMatch `json:"pending_matches"`
}
