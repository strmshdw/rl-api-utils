package ballchasing

import "encoding/json"

// UnmarshalJSON bridges flat list and nested detail formats seamlessly.
func (p *ReplayPlayer) UnmarshalJSON(data []byte) error {
	type Alias ReplayPlayer
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(p),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if p.Stats != nil {
		p.hydrateFromNestedStats()
	} else {
		p.hydrateFromFlatStats()
	}

	return nil
}

func (p *ReplayPlayer) hydrateFromNestedStats() {
	if p.Stats.Core != nil {
		if p.Core == nil {
			p.Core = p.Stats.Core
		}
		if p.Score == 0 {
			p.Score = p.Stats.Core.Score
		}
		if p.Goals == 0 {
			p.Goals = p.Stats.Core.Goals
		}
		if p.Assists == 0 {
			p.Assists = p.Stats.Core.Assists
		}
		if p.Saves == 0 {
			p.Saves = p.Stats.Core.Saves
		}
		if p.Shots == 0 {
			p.Shots = p.Stats.Core.Shots
		}
		if !p.MVP && p.Stats.Core.MVP {
			p.MVP = true
		}
	}
	if p.Stats.Boost != nil && p.Boost == nil {
		p.Boost = p.Stats.Boost
	}
	if p.Stats.Movement != nil && p.Movement == nil {
		p.Movement = p.Stats.Movement
	}
	if p.Stats.Positioning != nil && p.Positioning == nil {
		p.Positioning = p.Stats.Positioning
	}
	if p.Stats.Demo != nil {
		if p.Demo == nil {
			p.Demo = p.Stats.Demo
		}
		if p.Demos.Inflicted == 0 {
			p.Demos.Inflicted = p.Stats.Demo.Inflicted
		}
		if p.Demos.Taken == 0 {
			p.Demos.Taken = p.Stats.Demo.Taken
		}
	}
	if p.Stats.Ball != nil && p.Ball == nil {
		p.Ball = p.Stats.Ball
	}
}

func (p *ReplayPlayer) hydrateFromFlatStats() {
	hasPointers := p.Core != nil || p.Boost != nil || p.Movement != nil || p.Positioning != nil || p.Demo != nil || p.Ball != nil
	if hasPointers {
		p.Stats = &PlayerTelemetry{
			Core:        p.Core,
			Boost:       p.Boost,
			Movement:    p.Movement,
			Positioning: p.Positioning,
			Demo:        p.Demo,
			Ball:        p.Ball,
		}
	} else if p.Score > 0 || p.Goals > 0 || p.Assists > 0 || p.Saves > 0 || p.Shots > 0 {
		p.Core = &CoreStats{
			Score:   p.Score,
			Goals:   p.Goals,
			Assists: p.Assists,
			Saves:   p.Saves,
			Shots:   p.Shots,
			MVP:     p.MVP,
		}
		p.Stats = &PlayerTelemetry{Core: p.Core}
	}
	if p.Demo == nil && (p.Demos.Inflicted > 0 || p.Demos.Taken > 0) {
		p.Demo = &DemoStats{
			Inflicted: p.Demos.Inflicted,
			Taken:     p.Demos.Taken,
		}
		if p.Stats == nil {
			p.Stats = &PlayerTelemetry{}
		}
		p.Stats.Demo = p.Demo
	}
}

// UnmarshalJSON implements json.Unmarshaler for ReplayTeam, hydrating top-level
// Goals from nested stats.core.goals or the sum of player goals when top-level
// goals is omitted (as in Ballchasing's single replay detail endpoint).
func (t *ReplayTeam) UnmarshalJSON(data []byte) error {
	type Alias ReplayTeam
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(t),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if t.Goals == 0 && t.Stats != nil && t.Stats.Core != nil && t.Stats.Core.Goals > 0 {
		t.Goals = t.Stats.Core.Goals
	}
	if t.Goals == 0 && len(t.Players) > 0 {
		sum := 0
		for _, p := range t.Players {
			sum += p.Goals
		}
		if sum > 0 {
			t.Goals = sum
		}
	}
	return nil
}

// UnmarshalJSON bridges field differences between list and detail replay objects.
func (r *ReplayItem) UnmarshalJSON(data []byte) error {
	type Alias ReplayItem
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(r),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if r.Title == "" && r.ReplayTitle != "" {
		r.Title = r.ReplayTitle
	} else if r.ReplayTitle == "" && r.Title != "" {
		r.ReplayTitle = r.Title
	}

	if r.MatchGUID == "" && r.RocketLeagueID != "" {
		r.MatchGUID = r.RocketLeagueID
	} else if r.RocketLeagueID == "" && r.MatchGUID != "" {
		r.RocketLeagueID = r.MatchGUID
	}

	if r.Blue.Goals == 0 && r.Orange.Stats != nil && r.Orange.Stats.Core != nil && r.Orange.Stats.Core.GoalsAgainst > 0 {
		r.Blue.Goals = r.Orange.Stats.Core.GoalsAgainst
	}
	if r.Orange.Goals == 0 && r.Blue.Stats != nil && r.Blue.Stats.Core != nil && r.Blue.Stats.Core.GoalsAgainst > 0 {
		r.Orange.Goals = r.Blue.Stats.Core.GoalsAgainst
	}

	return nil
}
