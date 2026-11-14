package parity_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"unicode/utf16"

	"github.com/dank/rl-api-utils/internal/replay"
)

type ue3Writer struct{ buf bytes.Buffer }

func (w *ue3Writer) i32(v int32)   { _ = binary.Write(&w.buf, binary.LittleEndian, v) }
func (w *ue3Writer) i64(v int64)   { _ = binary.Write(&w.buf, binary.LittleEndian, v) }
func (w *ue3Writer) f32(v float32) { w.i32(int32(math.Float32bits(v))) }

func (w *ue3Writer) fstr(s string) {
	if s == "" {
		w.i32(0)
		return
	}
	w.i32(int32(len(s) + 1))
	w.buf.WriteString(s)
	w.buf.WriteByte(0)
}

func (w *ue3Writer) fstrUTF16(s string) {
	if s == "" {
		w.i32(0)
		return
	}
	units := append(utf16.Encode([]rune(s)), 0)
	w.i32(-int32(len(units)))
	for _, u := range units {
		_ = binary.Write(&w.buf, binary.LittleEndian, u)
	}
}

func (w *ue3Writer) propHdr(name, typ string, size int64) { w.fstr(name); w.fstr(typ); w.i64(size) }
func (w *ue3Writer) intProp(name string, v int32)         { w.propHdr(name, "IntProperty", 4); w.i32(v) }
func (w *ue3Writer) floatProp(name string, v float32)     { w.propHdr(name, "FloatProperty", 4); w.f32(v) }
func (w *ue3Writer) qwordProp(name string, v int64)       { w.propHdr(name, "QWordProperty", 8); w.i64(v) }

func (w *ue3Writer) boolProp(name string, v bool) {
	w.propHdr(name, "BoolProperty", 0)
	if v {
		w.buf.WriteByte(1)
	} else {
		w.buf.WriteByte(0)
	}
}

func (w *ue3Writer) strProp(name, v string, utf16LE bool) {
	var tmp ue3Writer
	if utf16LE {
		tmp.fstrUTF16(v)
	} else {
		tmp.fstr(v)
	}
	w.propHdr(name, "StrProperty", int64(tmp.buf.Len()))
	w.buf.Write(tmp.buf.Bytes())
}

func (w *ue3Writer) byteEnum(name, enumType, enumVal string) {
	var tmp ue3Writer
	tmp.fstr(enumVal)
	w.propHdr(name, "ByteProperty", int64(tmp.buf.Len()))
	w.fstr(enumType)
	w.buf.Write(tmp.buf.Bytes())
}

func (w *ue3Writer) byteRaw(name string, raw ...byte) {
	w.propHdr(name, "ByteProperty", int64(len(raw)))
	w.fstr("None")
	w.buf.Write(raw)
}

func (w *ue3Writer) structProp(name, typ string, payload []byte) {
	w.propHdr(name, "StructProperty", int64(len(payload)))
	w.fstr(typ)
	w.buf.Write(payload)
}

func (w *ue3Writer) arrayProp(name string, count int32, payload []byte) {
	w.propHdr(name, "ArrayProperty", int64(4+len(payload)))
	w.i32(count)
	w.buf.Write(payload)
}

func buildReplay(engVer, licVer int32, netVer *int32, className string, props []byte) []byte {
	var hdr, out ue3Writer
	hdr.i32(engVer)
	hdr.i32(licVer)
	if netVer != nil {
		hdr.i32(*netVer)
	}
	hdr.fstr(className)
	hdr.buf.Write(props)
	out.i32(int32(hdr.buf.Len()))
	out.i32(0x12345678)
	out.buf.Write(hdr.buf.Bytes())
	out.buf.Write([]byte{0xde, 0xad, 0xbe, 0xef}) // trailing replay body
	return out.buf.Bytes()
}

func TestParity_ModernNetVersionReplayRoundTrip(t *testing.T) {
	var p1ID, p1, p2ID, p2, p3, players, root ue3Writer
	p1ID.qwordProp("Uid", 76561198012345678)
	p1ID.byteEnum("Platform", "OnlinePlatform", "OnlinePlatform_Steam")
	p1ID.fstr("None")

	p1.strProp("Name", "Jäger🏆", true) // UTF-16LE with surrogate emoji
	p1.intProp("Team", 0)
	p1.intProp("Score", 740)
	p1.intProp("Goals", 3)
	p1.intProp("Assists", 1)
	p1.intProp("Saves", 2)
	p1.intProp("Shots", 5)
	p1.intProp("Demos", 1)
	p1.boolProp("bBot", false)
	p1.structProp("PlayerID", "UniqueNetId", p1ID.buf.Bytes())
	p1.fstr("None")

	p2ID.strProp("EpicAccountId", "epic-user-42", false)
	p2ID.byteEnum("Platform", "OnlinePlatform", "OnlinePlatform_EOS")
	p2ID.fstr("None")

	p2.strProp("Name", "EpicMate", false)
	p2.intProp("Team", 1)
	p2.intProp("Score", 420)
	p2.intProp("Goals", 2)
	p2.intProp("Demolitions", 3)
	p2.structProp("PlayerID", "UniqueNetId", p2ID.buf.Bytes())
	p2.fstr("None")

	p3.strProp("Name", "Maverick", false)
	p3.intProp("Team", 1)
	p3.boolProp("bBot", true)
	// Raw binary UniqueNetId starting with bytes that mimic a huge FString length
	p3.structProp("PlayerID", "UniqueNetId", []byte{0x00, 0x00, 0x02, 0x00, 0xaa, 0xbb, 0xcc, 0xdd})
	p3.fstr("None")

	players.buf.Write(p1.buf.Bytes())
	players.buf.Write(p2.buf.Bytes())
	players.buf.Write(p3.buf.Bytes())

	root.strProp("Id", "GUID-MODERN-868", false)
	root.strProp("MapName", "Stadium_P", false)
	root.intProp("Team0Score", 3)
	root.intProp("Team1Score", 2)
	root.intProp("TeamSize", 2)
	root.floatProp("TotalSecondsPlayed", 308.5)
	root.boolProp("bForfeit", false)
	root.boolProp("bOvertime", true)
	root.strProp("Date", "2026-10-16 08-10-36", false)
	root.byteRaw("PrimaryPlayerTeam", 0)
	root.strProp("PlayerName", "Jäger🏆", true)
	root.arrayProp("Goals", 2, []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06})
	root.structProp("CameraPreset", "Vector", []byte{0x00, 0x00, 0x80, 0x3f})
	root.arrayProp("PlayerStats", 3, players.buf.Bytes())
	root.fstr("None")

	netVer := int32(10)
	raw := buildReplay(868, 26, &netVer, "TAGame.Replay_Soccar_TA", root.buf.Bytes())

	winner, localTeam := 0, 0
	want := &replay.ReplayMetadata{
		MatchGUID: "GUID-MODERN-868", MapName: "Stadium_P", Team0Score: 3, Team1Score: 2,
		WinnerTeam: &winner, TeamSize: 2, TotalSeconds: 308.5, Overtime: true,
		Date: "2026-10-16T08:10:36Z", LocalTeam: &localTeam, PlayerName: "Jäger🏆",
		Players: []replay.ReplayPlayerStats{
			{Name: "Jäger🏆", Platform: "Steam", OnlineID: "76561198012345678", Team: 0, Score: 740, Goals: 3, Assists: 1, Saves: 2, Shots: 5, Demos: 1},
			{Name: "EpicMate", Platform: "Epic", OnlineID: "epic-user-42", Team: 1, Score: 420, Goals: 2, Demos: 3},
			{Name: "Maverick", Platform: "Unknown", Team: 1, IsBot: true},
		},
	}

	got, err := replay.ParseReplay(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseReplay failed: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("modern round-trip mismatch:\ngot:  %+v\nwant: %+v", got, want)
	}

	filePath := filepath.Join(t.TempDir(), "modern.replay")
	if err := os.WriteFile(filePath, raw, 0o600); err != nil {
		t.Fatalf("write temp replay: %v", err)
	}
	gotFile, err := replay.ParseReplayFile(filePath)
	if err != nil || !reflect.DeepEqual(gotFile, want) {
		t.Fatalf("ParseReplayFile mismatch (err=%v):\ngot:  %+v\nwant: %+v", err, gotFile, want)
	}
}

func TestParity_LegacyHeaderVersionsWithoutNetVersion(t *testing.T) {
	versions := []struct {
		name           string
		engVer, licVer int32
	}{
		{"legacy engVer<868 licVer<18", 867, 17},
		{"boundary engVer=868 licVer=17 omits netVer", 868, 17},
	}

	for _, ver := range versions {
		t.Run(ver.name, func(t *testing.T) {
			var p, root ue3Writer
			p.strProp("Name", "ConsoleStar", true)
			p.intProp("Team", 1)
			p.byteEnum("Platform", "OnlinePlatform", "OnlinePlatform_PS4")
			p.qwordProp("OnlineID", 987654321)
			p.fstr("None")

			root.strProp("Id", "GUID-LEGACY", false)
			root.strProp("MapName", "Park_P", false)
			root.intProp("Team0Score", 2)
			root.intProp("Team1Score", 3)
			root.intProp("TeamSize", 1)
			root.floatProp("TotalSecondsPlayed", 342.0) // heuristic overtime
			root.qwordProp("MatchStartEpoch", 1729066236)
			root.intProp("PrimaryPlayerTeam", 1)
			root.strProp("PlayerName", "ConsoleStar", true)
			root.arrayProp("PlayerStats", 1, p.buf.Bytes())
			root.fstr("None")

			raw := buildReplay(ver.engVer, ver.licVer, nil, "TAGame.Replay_Soccar_TA", root.buf.Bytes())
			got, err := replay.ParseReplay(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("ParseReplay(%s) failed: %v", ver.name, err)
			}

			winner, localTeam := 1, 1
			want := &replay.ReplayMetadata{
				MatchGUID: "GUID-LEGACY", MapName: "Park_P", Team0Score: 2, Team1Score: 3,
				WinnerTeam: &winner, TeamSize: 1, TotalSeconds: 342.0, Overtime: true,
				Date: "2024-10-16T08:10:36Z", LocalTeam: &localTeam, PlayerName: "ConsoleStar",
				Players: []replay.ReplayPlayerStats{
					{Name: "ConsoleStar", Platform: "PlayStation", OnlineID: "987654321", Team: 1},
				},
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("legacy round-trip mismatch:\ngot:  %+v\nwant: %+v", got, want)
			}
		})
	}
}

func TestParity_MalformedAndTruncatedBuffers(t *testing.T) {
	makeCase := func(appendFn func(*ue3Writer)) []byte {
		var w ue3Writer
		w.i32(64)
		w.i32(0)
		w.i32(868)
		w.i32(20)
		w.i32(1)
		w.fstr("TAGame.Replay_Soccar_TA")
		appendFn(&w)
		return w.buf.Bytes()
	}

	cases := []struct {
		name string
		data []byte
	}{
		{"ascii fstring exceeds remaining bytes", makeCase(func(w *ue3Writer) { w.i32(60000) })},
		{"utf16 fstring exceeds remaining bytes", makeCase(func(w *ue3Writer) { w.i32(-60000) })},
		{"byte property dataSize exceeds remaining", makeCase(func(w *ue3Writer) { w.propHdr("PrimaryPlayerTeam", "ByteProperty", 512*1024); w.fstr("None") })},
		{"player stats count exceeds remaining", makeCase(func(w *ue3Writer) { w.propHdr("PlayerStats", "ArrayProperty", 16); w.i32(500) })},
		{"skipped array payload exceeds remaining", makeCase(func(w *ue3Writer) { w.propHdr("Goals", "ArrayProperty", 1024); w.i32(1) })},
		{"truncated before netVersion", makeCase(func(*ue3Writer) {})[:16]},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := replay.ParseReplay(bytes.NewReader(tc.data)); err == nil {
				t.Fatalf("expected error for malformed case %q, got nil", tc.name)
			}
		})
	}
}
