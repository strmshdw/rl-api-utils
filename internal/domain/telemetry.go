package domain

// CameraSettings stores player camera configuration settings.
type CameraSettings struct {
	FOV             int     `json:"fov"`
	Height          int     `json:"height"`
	Pitch           int     `json:"pitch"`
	Distance        int     `json:"distance"`
	Stiffness       float64 `json:"stiffness"`
	SwivelSpeed     float64 `json:"swivel_speed"`
	TransitionSpeed float64 `json:"transition_speed"`
}

// PlayerRank represents rank and tier information attached to replay telemetry.
type PlayerRank struct {
	ID        string  `json:"id,omitempty"`
	Tier      int     `json:"tier,omitempty"`
	Division  int     `json:"division,omitempty"`
	Name      string  `json:"name,omitempty"`
	ShortRank string  `json:"short_rank,omitempty"`
	MMR       float64 `json:"mmr,omitempty"`
}

// CoreStats represents standard and extended mechanics counters from Ballchasing telemetry.
type CoreStats struct {
	Shots              int     `json:"shots"`
	ShotsAgainst       int     `json:"shots_against"`
	Goals              int     `json:"goals"`
	GoalsAgainst       int     `json:"goals_against"`
	Saves              int     `json:"saves"`
	Assists            int     `json:"assists"`
	Score              int     `json:"score"`
	MVP                bool    `json:"mvp"`
	ShootingPercentage float64 `json:"shooting_percentage"`
	EpicSaves          int     `json:"epic_saves"`
	Clears             int     `json:"clears"`
	Centers            int     `json:"centers"`
	AerialHits         int     `json:"aerial_hits"`
	FirstTouches       int     `json:"first_touches"`
	FlipResets         int     `json:"flip_resets"`
	CrossbarHits       int     `json:"crossbar_hits"`
	BicycleHits        int     `json:"bicycle_hits"`
	JuggleHits         int     `json:"juggle_hits"`
}

// BoostStats captures boost economy, steals, and distribution metrics.
type BoostStats struct {
	BPM                       float64 `json:"bpm"`
	BCPM                      float64 `json:"bcpm"`
	AvgAmount                 float64 `json:"avg_amount"`
	AmountCollected           int     `json:"amount_collected"`
	AmountStolen              int     `json:"amount_stolen"`
	AmountCollectedBig        int     `json:"amount_collected_big"`
	AmountStolenBig           int     `json:"amount_stolen_big"`
	AmountCollectedSmall      int     `json:"amount_collected_small"`
	AmountStolenSmall         int     `json:"amount_stolen_small"`
	CountCollectedBig         int     `json:"count_collected_big"`
	CountStolenBig            int     `json:"count_stolen_big"`
	CountCollectedSmall       int     `json:"count_collected_small"`
	CountStolenSmall          int     `json:"count_stolen_small"`
	AmountOverfill            int     `json:"amount_overfill"`
	AmountOverfillStolen      int     `json:"amount_overfill_stolen"`
	AmountUsedWhileSupersonic int     `json:"amount_used_while_supersonic"`
	TimeZeroBoost             float64 `json:"time_zero_boost"`
	PercentZeroBoost          float64 `json:"percent_zero_boost"`
	TimeFullBoost             float64 `json:"time_full_boost"`
	PercentFullBoost          float64 `json:"percent_full_boost"`
	TimeBoost0To25            float64 `json:"time_boost_0_25"`
	TimeBoost25To50           float64 `json:"time_boost_25_50"`
	TimeBoost50To75           float64 `json:"time_boost_50_75"`
	TimeBoost75To100          float64 `json:"time_boost_75_100"`
	PercentBoost0To25         float64 `json:"percent_boost_0_25"`
	PercentBoost25To50        float64 `json:"percent_boost_25_50"`
	PercentBoost50To75        float64 `json:"percent_boost_50_75"`
	PercentBoost75To100       float64 `json:"percent_boost_75_100"`
}

// MovementStats measures speed, distance, elevation, and powerslide mechanics.
type MovementStats struct {
	AvgSpeed              float64 `json:"avg_speed"`
	TotalDistance         float64 `json:"total_distance"`
	TimeSupersonicSpeed   float64 `json:"time_supersonic_speed"`
	TimeBoostSpeed        float64 `json:"time_boost_speed"`
	TimeSlowSpeed         float64 `json:"time_slow_speed"`
	TimeGround            float64 `json:"time_ground"`
	TimeLowAir            float64 `json:"time_low_air"`
	TimeHighAir           float64 `json:"time_high_air"`
	TimePowerslide        float64 `json:"time_powerslide"`
	CountPowerslide       int     `json:"count_powerslide"`
	AvgPowerslideDuration float64 `json:"avg_powerslide_duration"`
	AvgSpeedPercentage    float64 `json:"avg_speed_percentage"`
	PercentSlowSpeed      float64 `json:"percent_slow_speed"`
	PercentBoostSpeed     float64 `json:"percent_boost_speed"`
	PercentSupersonic     float64 `json:"percent_supersonic_speed"`
	PercentGround         float64 `json:"percent_ground"`
	PercentLowAir         float64 `json:"percent_low_air"`
	PercentHighAir        float64 `json:"percent_high_air"`
}

// PositioningStats quantifies field positioning, spacing, and third/half allocations.
type PositioningStats struct {
	AvgDistanceToBall             float64 `json:"avg_distance_to_ball"`
	AvgDistanceToBallPossession   float64 `json:"avg_distance_to_ball_possession"`
	AvgDistanceToBallNoPossession float64 `json:"avg_distance_to_ball_no_possession"`
	AvgDistanceToMates            float64 `json:"avg_distance_to_mates"`
	TimeDefensiveThird            float64 `json:"time_defensive_third"`
	TimeNeutralThird              float64 `json:"time_neutral_third"`
	TimeOffensiveThird            float64 `json:"time_offensive_third"`
	TimeDefensiveHalf             float64 `json:"time_defensive_half"`
	TimeOffensiveHalf             float64 `json:"time_offensive_half"`
	TimeBehindBall                float64 `json:"time_behind_ball"`
	TimeInFrontOfBall             float64 `json:"time_infront_ball"`
	TimeMostBack                  float64 `json:"time_most_back"`
	TimeMostForward               float64 `json:"time_most_forward"`
	GoalsAgainstWhileLastDefender int     `json:"goals_against_while_last_defender"`
	TimeClosestToBall             float64 `json:"time_closest_to_ball"`
	TimeFarthestFromBall          float64 `json:"time_farthest_from_ball"`
	PercentDefensiveThird         float64 `json:"percent_defensive_third"`
	PercentOffensiveThird         float64 `json:"percent_offensive_third"`
	PercentNeutralThird           float64 `json:"percent_neutral_third"`
	PercentDefensiveHalf          float64 `json:"percent_defensive_half"`
	PercentOffensiveHalf          float64 `json:"percent_offensive_half"`
	PercentBehindBall             float64 `json:"percent_behind_ball"`
	PercentInFrontOfBall          float64 `json:"percent_infront_ball"`
	PercentMostBack               float64 `json:"percent_most_back"`
	PercentMostForward            float64 `json:"percent_most_forward"`
	PercentClosestToBall          float64 `json:"percent_closest_to_ball"`
	PercentFarthestFromBall       float64 `json:"percent_farthest_from_ball"`
}

// DemoStats details inflicted and taken demolitions with field-half splits.
type DemoStats struct {
	Inflicted                int `json:"inflicted"`
	InflictedInDefensiveHalf int `json:"inflicted_in_defensive_half"`
	InflictedInOffensiveHalf int `json:"inflicted_in_offensive_half"`
	Taken                    int `json:"taken"`
	TakenInDefensiveHalf     int `json:"taken_in_defensive_half"`
	TakenInOffensiveHalf     int `json:"taken_in_offensive_half"`
}

// BallStats measures ball possession and territorial occupation.
type BallStats struct {
	PossessionTime float64 `json:"possession_time"`
	TimeInSide     float64 `json:"time_in_side"`
}
