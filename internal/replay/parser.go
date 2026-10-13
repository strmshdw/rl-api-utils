package replay

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
)

// ReplayPlayerStats contains per-player statistics extracted from a .replay file.
type ReplayPlayerStats struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
	OnlineID string `json:"online_id"`
	Team     int    `json:"team"` // 0 = Blue, 1 = Orange
	Score    int    `json:"score"`
	Goals    int    `json:"goals"`
	Assists  int    `json:"assists"`
	Saves    int    `json:"saves"`
	Shots    int    `json:"shots"`
	IsBot    bool   `json:"is_bot"`
}

// ReplayMetadata contains game-level metadata and box scores extracted from a .replay file.
type ReplayMetadata struct {
	MatchGUID    string              `json:"match_guid"`
	MapName      string              `json:"map_name"`
	Team0Score   int                 `json:"team0_score"` // Blue
	Team1Score   int                 `json:"team1_score"` // Orange
	WinnerTeam   *int                `json:"winner_team,omitempty"`
	TeamSize     int                 `json:"team_size"`
	TotalSeconds float32             `json:"total_seconds"`
	Forfeit      bool                `json:"forfeit"`
	Overtime     bool                `json:"overtime"`
	Date         string              `json:"date"`
	Players      []ReplayPlayerStats `json:"players"`
	LocalTeam    *int                `json:"local_team,omitempty"`
	PlayerName   string              `json:"player_name,omitempty"` // Local player name in replay
}

// ParseReplayFile reads and parses the header of a Rocket League .replay file from disk.
func ParseReplayFile(filePath string) (*ReplayMetadata, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("open replay: %w", err)
	}

	return ParseReplay(bytes.NewReader(data))
}

// ParseReplay reads and parses the header properties from an io.ReadSeeker.
func ParseReplay(r io.ReadSeeker) (*ReplayMetadata, error) {
	var headerLen, crc, engVer, licVer int32
	if err := binary.Read(r, binary.LittleEndian, &headerLen); err != nil {
		return nil, fmt.Errorf("read headerLen: %w", err)
	}
	if headerLen <= 0 || headerLen > 32*1024*1024 {
		return nil, fmt.Errorf("invalid headerLen: %d", headerLen)
	}
	if err := binary.Read(r, binary.LittleEndian, &crc); err != nil {
		return nil, fmt.Errorf("read crc: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &engVer); err != nil {
		return nil, fmt.Errorf("read engineVersion: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &licVer); err != nil {
		return nil, fmt.Errorf("read licenseeVersion: %w", err)
	}

	if engVer >= 868 && licVer >= 18 {
		var netVer int32
		if err := binary.Read(r, binary.LittleEndian, &netVer); err != nil {
			return nil, fmt.Errorf("read netVersion: %w", err)
		}
	}

	className, err := readFString(r)
	if err != nil {
		return nil, fmt.Errorf("read className: %w", err)
	}
	if !strings.HasPrefix(className, "TAGame.") {
		return nil, fmt.Errorf("unrecognized replay class %q", className)
	}

	meta := &ReplayMetadata{}

	props, err := parsePropertyMap(r)
	if err != nil {
		return nil, fmt.Errorf("parse property map: %w", err)
	}

	populateMetadata(meta, props)

	// Determine winner
	if meta.Team0Score > meta.Team1Score {
		w := 0
		meta.WinnerTeam = &w
	} else if meta.Team1Score > meta.Team0Score {
		w := 1
		meta.WinnerTeam = &w
	}

	return meta, nil
}

func readFString(r io.Reader) (string, error) {
	var length int32
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return "", err
	}
	if length == 0 {
		return "", nil
	}

	const maxStrLen = 65536
	if length > maxStrLen || length < -maxStrLen {
		return "", fmt.Errorf("invalid fstring length %d (exceeds max %d)", length, maxStrLen)
	}

	if length > 0 {
		buf := make([]byte, length)
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", err
		}
		// String is null terminated
		return strings.TrimRight(string(buf), "\x00"), nil
	}

	// Negative length indicates UTF-16LE
	utfLen := -length * 2
	buf := make([]byte, utfLen)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	// Decode UTF-16LE to string
	u16 := make([]uint16, -length)
	for i := 0; i < len(u16); i++ {
		u16[i] = binary.LittleEndian.Uint16(buf[i*2 : (i+1)*2])
	}
	runes := make([]rune, 0, len(u16))
	for _, v := range u16 {
		if v == 0 {
			break
		}
		runes = append(runes, rune(v))
	}
	return string(runes), nil
}

type propertyValue struct {
	typeName string
	intVal   int
	strVal   string
	floatVal float32
	boolVal  bool
	int64Val int64
	subProps []map[string]*propertyValue
}

func parsePropertyMap(r io.ReadSeeker) (map[string]*propertyValue, error) {
	props := make(map[string]*propertyValue)

	for {
		name, err := readFString(r)
		if err != nil {
			return nil, err
		}
		name = strings.TrimSpace(name)
		if name == "" || name == "None" {
			break
		}

		typeName, err := readFString(r)
		if err != nil {
			return nil, err
		}
		typeName = strings.TrimSpace(typeName)

		var dataSize int64
		if err := binary.Read(r, binary.LittleEndian, &dataSize); err != nil {
			return nil, err
		}
		if dataSize < 0 || dataSize > 16*1024*1024 {
			return nil, fmt.Errorf("invalid property dataSize %d", dataSize)
		}

		pv := &propertyValue{typeName: typeName}

		switch typeName {
		case "IntProperty":
			var v int32
			if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
				return nil, err
			}
			pv.intVal = int(v)

		case "StrProperty", "NameProperty":
			s, err := readFString(r)
			if err != nil {
				return nil, err
			}
			pv.strVal = s

		case "FloatProperty":
			var v float32
			if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
				return nil, err
			}
			pv.floatVal = v

		case "BoolProperty":
			var b byte
			if err := binary.Read(r, binary.LittleEndian, &b); err != nil {
				return nil, err
			}
			pv.boolVal = (b != 0)

		case "QWordProperty":
			var v int64
			if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
				return nil, err
			}
			pv.int64Val = v

		case "ByteProperty":
			enumType, err := readFString(r)
			if err != nil {
				return nil, err
			}
			if dataSize > 0 {
				if enumType != "" && enumType != "None" {
					enumVal, _ := readFString(r)
					pv.strVal = enumVal
				} else {
					if dataSize > 1024*1024 {
						return nil, fmt.Errorf("invalid byte property dataSize %d", dataSize)
					}
					buf := make([]byte, dataSize)
					io.ReadFull(r, buf)
					if len(buf) > 0 {
						pv.intVal = int(buf[0])
					}
				}
			}

		case "ArrayProperty":
			var count int32
			if err := binary.Read(r, binary.LittleEndian, &count); err != nil {
				return nil, err
			}
			if count < 0 || count > 1000 {
				return nil, fmt.Errorf("invalid array count %d", count)
			}
			if name == "PlayerStats" {
				for i := int32(0); i < count; i++ {
					sub, err := parsePropertyMap(r)
					if err != nil {
						break
					}
					pv.subProps = append(pv.subProps, sub)
				}
			} else {
				rem := dataSize - 4
				if rem > 0 {
					r.Seek(rem, io.SeekCurrent)
				}
			}

		case "StructProperty":
			structType, err := readFString(r)
			if err != nil {
				return nil, err
			}
			if structType == "UniqueNetId" {
				startPos, _ := r.Seek(0, io.SeekCurrent)
				innerProps, err := parsePropertyMap(r)
				if err == nil {
					pv.subProps = []map[string]*propertyValue{innerProps}
				}
				r.Seek(startPos+dataSize, io.SeekStart)
			} else {
				if dataSize > 0 {
					r.Seek(dataSize, io.SeekCurrent)
				}
			}

		default:
			if dataSize > 0 {
				r.Seek(dataSize, io.SeekCurrent)
			}
		}

		props[name] = pv
	}

	return props, nil
}

func populateMetadata(meta *ReplayMetadata, props map[string]*propertyValue) {
	if v, ok := props["Id"]; ok {
		meta.MatchGUID = v.strVal
	}
	if v, ok := props["MapName"]; ok {
		meta.MapName = v.strVal
	}
	if v, ok := props["Team0Score"]; ok {
		meta.Team0Score = v.intVal
	}
	if v, ok := props["Team1Score"]; ok {
		meta.Team1Score = v.intVal
	}
	if v, ok := props["TeamSize"]; ok {
		meta.TeamSize = v.intVal
	}
	if v, ok := props["TotalSecondsPlayed"]; ok {
		meta.TotalSeconds = v.floatVal
	}
	if v, ok := props["bForfeit"]; ok {
		meta.Forfeit = v.boolVal
	}
	if v, ok := props["Date"]; ok {
		meta.Date = v.strVal
	}
	if v, ok := props["PrimaryPlayerTeam"]; ok {
		t := v.intVal
		meta.LocalTeam = &t
	}
	if v, ok := props["PlayerName"]; ok {
		meta.PlayerName = v.strVal
	}

	if ps, ok := props["PlayerStats"]; ok && len(ps.subProps) > 0 {
		for _, sp := range ps.subProps {
			p := ReplayPlayerStats{}
			if n, ok := sp["Name"]; ok {
				p.Name = n.strVal
			}
			if sc, ok := sp["Score"]; ok {
				p.Score = sc.intVal
			}
			if g, ok := sp["Goals"]; ok {
				p.Goals = g.intVal
			}
			if a, ok := sp["Assists"]; ok {
				p.Assists = a.intVal
			}
			if s, ok := sp["Saves"]; ok {
				p.Saves = s.intVal
			}
			if sh, ok := sp["Shots"]; ok {
				p.Shots = sh.intVal
			}
			if tm, ok := sp["Team"]; ok {
				p.Team = tm.intVal
			}
			if bot, ok := sp["bBot"]; ok {
				p.IsBot = bot.boolVal
			}
			if plat, ok := sp["Platform"]; ok {
				p.Platform = cleanPlatformName(plat.strVal)
			}
			if pid, ok := sp["PlayerID"]; ok && len(pid.subProps) > 0 {
				inner := pid.subProps[0]
				if uid, ok := inner["Uid"]; ok && uid.int64Val != 0 {
					p.OnlineID = fmt.Sprintf("%d", uid.int64Val)
					if p.Platform == "" {
						p.Platform = "Steam"
					}
				}
				if epicID, ok := inner["EpicAccountId"]; ok && epicID.strVal != "" {
					p.OnlineID = epicID.strVal
					p.Platform = "Epic"
				}
				if plat, ok := inner["Platform"]; ok && plat.strVal != "" {
					p.Platform = cleanPlatformName(plat.strVal)
				}
			}
			if oid, ok := sp["OnlineID"]; ok {
				if oid.int64Val != 0 && p.OnlineID == "" {
					p.OnlineID = fmt.Sprintf("%d", oid.int64Val)
				} else if oid.strVal != "" && p.OnlineID == "" {
					p.OnlineID = oid.strVal
				}
			}

			if p.Platform == "" {
				p.Platform = "Unknown"
			}

			meta.Players = append(meta.Players, p)
		}
	}
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
