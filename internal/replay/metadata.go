package replay

import (
	"fmt"
	"strings"
	"time"
)

func populateMetadata(meta *ReplayMetadata, props map[string]*propertyValue) {
	if meta == nil || props == nil {
		return
	}
	if v, ok := props["Id"]; ok && v != nil {
		meta.MatchGUID = v.strVal
	}
	if v, ok := props["MapName"]; ok && v != nil {
		meta.MapName = v.strVal
	}
	if v, ok := props["Team0Score"]; ok && v != nil {
		meta.Team0Score = v.intVal
	}
	if v, ok := props["Team1Score"]; ok && v != nil {
		meta.Team1Score = v.intVal
	}
	if v, ok := props["TeamSize"]; ok && v != nil {
		meta.TeamSize = v.intVal
	}
	if v, ok := props["TotalSecondsPlayed"]; ok && v != nil {
		meta.TotalSeconds = v.floatVal
	}
	if v, ok := props["bForfeit"]; ok && v != nil {
		meta.Forfeit = v.boolVal
	}
	meta.Overtime = extractOvertime(meta, props)
	if v, ok := props["Date"]; ok && v != nil && strings.TrimSpace(v.strVal) != "" {
		meta.Date = normalizeReplayDate(v.strVal)
	} else if v, ok := props["MatchStartEpoch"]; ok && v != nil && v.int64Val > 0 {
		meta.Date = time.Unix(v.int64Val, 0).UTC().Format(time.RFC3339)
	}
	if v, ok := props["PrimaryPlayerTeam"]; ok && v != nil {
		t := v.intVal
		meta.LocalTeam = &t
	}
	if v, ok := props["PlayerName"]; ok && v != nil {
		meta.PlayerName = v.strVal
	}
	if ps, ok := props["PlayerStats"]; ok && ps != nil && len(ps.subProps) > 0 {
		for _, sp := range ps.subProps {
			meta.Players = append(meta.Players, extractPlayerStats(sp))
		}
	}
}

func extractPlayerStats(sp map[string]*propertyValue) ReplayPlayerStats {
	p := ReplayPlayerStats{}
	if n, ok := sp["Name"]; ok && n != nil {
		p.Name = n.strVal
	}
	if sc, ok := sp["Score"]; ok && sc != nil {
		p.Score = sc.intVal
	}
	if g, ok := sp["Goals"]; ok && g != nil {
		p.Goals = g.intVal
	}
	if a, ok := sp["Assists"]; ok && a != nil {
		p.Assists = a.intVal
	}
	if s, ok := sp["Saves"]; ok && s != nil {
		p.Saves = s.intVal
	}
	if sh, ok := sp["Shots"]; ok && sh != nil {
		p.Shots = sh.intVal
	}
	for _, key := range []string{"Demos", "Demolitions", "Demolition", "demos"} {
		if d, ok := sp[key]; ok && d != nil {
			p.Demos = d.intVal
			break
		}
	}
	if tm, ok := sp["Team"]; ok && tm != nil {
		p.Team = tm.intVal
	}
	if bot, ok := sp["bBot"]; ok && bot != nil {
		p.IsBot = bot.boolVal
	}
	if plat, ok := sp["Platform"]; ok && plat != nil {
		p.Platform = cleanPlatformName(plat.strVal)
	}
	if pid, ok := sp["PlayerID"]; ok && pid != nil && len(pid.subProps) > 0 {
		inner := pid.subProps[0]
		if uid, ok := inner["Uid"]; ok && uid != nil && uid.int64Val != 0 {
			p.OnlineID = fmt.Sprintf("%d", uid.int64Val)
			if p.Platform == "" {
				p.Platform = "Steam"
			}
		}
		if epicID, ok := inner["EpicAccountId"]; ok && epicID != nil && epicID.strVal != "" {
			p.OnlineID = epicID.strVal
			p.Platform = "Epic"
		}
		if plat, ok := inner["Platform"]; ok && plat != nil && plat.strVal != "" {
			p.Platform = cleanPlatformName(plat.strVal)
		}
	}
	if oid, ok := sp["OnlineID"]; ok && oid != nil && p.OnlineID == "" {
		if oid.int64Val != 0 {
			p.OnlineID = fmt.Sprintf("%d", oid.int64Val)
		} else if oid.strVal != "" {
			p.OnlineID = oid.strVal
		}
	}
	if p.Platform == "" {
		p.Platform = "Unknown"
	}
	return p
}

func cleanPlatformName(raw string) string {
	raw = strings.TrimPrefix(raw, "OnlinePlatform_")
	switch strings.ToLower(raw) {
	case "steam":
		return "Steam"
	case "epic", "eos":
		return "Epic"
	case "ps4", "ps5", "playstation", "psn":
		return "PlayStation"
	case "dingo", "xbox", "xboxone", "xboxseries":
		return "Xbox"
	case "switch", "nintendo":
		return "Switch"
	default:
		return raw
	}
}

// normalizeReplayDate parses various Rocket League replay header date layouts
// and normalizes them into canonical UTC RFC3339 format.
func normalizeReplayDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	layouts := []string{
		"2006-01-02 15-04-05", // Standard Psyonix replay header format
		"2006-01-02:15-04",    // Older / PsyNet match history format
		"2006-01-02 15:04:05", // Standard space-separated datetime
		"2006-01-02T15:04:05", // ISO-8601 without offset
		time.RFC3339,          // Canonical RFC3339
		time.RFC3339Nano,      // RFC3339 with nanoseconds
		"2006-01-02",          // Date-only fallback
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, raw); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return raw // Defensive fallback: retain raw string if unrecognized
}

// extractOvertime determines whether a match went into overtime based on explicit
// header properties or telemetry indicator heuristics.
func extractOvertime(meta *ReplayMetadata, props map[string]*propertyValue) bool {
	for _, key := range []string{"bOvertime", "bOverTime", "Overtime", "overtime"} {
		if v, ok := props[key]; ok && v != nil {
			return v.boolVal
		}
	}
	if v, ok := props["OvertimeSeconds"]; ok && v != nil {
		if v.intVal > 0 || v.floatVal > 0 {
			return true
		}
	}
	if meta == nil {
		return false
	}

	scoreDiff := meta.Team0Score - meta.Team1Score
	if scoreDiff < 0 {
		scoreDiff = -scoreDiff
	}
	return meta.TotalSeconds >= minOvertimeHeuristicSeconds && !meta.Forfeit && scoreDiff == 1
}
