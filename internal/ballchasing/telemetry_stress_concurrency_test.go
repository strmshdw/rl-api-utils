package ballchasing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"testing"
)

func TestStress_Bidirectional_RoundTripStability(t *testing.T) {
	t.Run("nested detail payload round trip idempotency", func(t *testing.T) {
		var d1 ReplayDetail
		if err := json.Unmarshal([]byte(canonicalNestedFixture), &d1); err != nil {
			t.Fatalf("first unmarshal failed: %v", err)
		}
		m1, err := json.Marshal(d1)
		if err != nil {
			t.Fatalf("first marshal failed: %v", err)
		}
		var d2 ReplayDetail
		if err := json.Unmarshal(m1, &d2); err != nil {
			t.Fatalf("second unmarshal failed: %v", err)
		}
		m2, err := json.Marshal(d2)
		if err != nil {
			t.Fatalf("second marshal failed: %v", err)
		}
		if !bytes.Equal(m1, m2) {
			t.Fatalf("bidirectional round trip serialization is NOT idempotent!")
		}
		if d2.ID != d1.ID || d2.Title != d1.Title || d2.Duration != d1.Duration || d2.OvertimeSeconds != d1.OvertimeSeconds {
			t.Errorf("metadata altered after round-trip")
		}
		p1 := d1.Blue.Players[0]
		p2 := d2.Blue.Players[0]
		if p2.Score != p1.Score || p2.Goals != p1.Goals || p2.Assists != p1.Assists || p2.Core == nil || p2.Core.AerialHits != 14 {
			t.Errorf("player telemetry altered after round trip")
		}
	})

	t.Run("flat list payload round trip and Core synthesis preservation", func(t *testing.T) {
		flatJSON := `{
			"id": "flat-roundtrip-1",
			"replay_title": "Game Flat",
			"blue": {
				"players": [{
					"name": "FlatStar", "score": 620, "goals": 2, "assists": 1, "saves": 3, "shots": 4,
					"demos": { "inflicted": 1, "taken": 0 }
				}]
			}
		}`

		var r1 ReplayDetail
		if err := json.Unmarshal([]byte(flatJSON), &r1); err != nil {
			t.Fatalf("unmarshal flat JSON failed: %v", err)
		}
		if r1.Title != "Game Flat" || r1.Blue.Players[0].Core == nil || r1.Blue.Players[0].Core.Score != 620 {
			t.Fatalf("synthesized Core missing on r1")
		}

		marshaled, err := json.Marshal(r1)
		if err != nil {
			t.Fatalf("marshal r1 failed: %v", err)
		}
		var r2 ReplayDetail
		if err := json.Unmarshal(marshaled, &r2); err != nil {
			t.Fatalf("unmarshal marshaled flat failed: %v", err)
		}
		p2 := r2.Blue.Players[0]
		if p2.Score != 620 || p2.Goals != 2 || p2.Saves != 3 || p2.Core == nil || p2.Core.Score != 620 {
			t.Errorf("p2 box score or synthesized Core corrupted: %+v", p2)
		}
	})
}

func TestStress_ZeroFieldClobbering(t *testing.T) {
	t.Run("legitimate 0-score player with non-zero nested core stats", func(t *testing.T) {
		payload := `{
			"name": "HydratedPlayer", "score": 0, "goals": 0, "assists": 0, "saves": 0, "shots": 0,
			"stats": { "core": { "score": 450, "goals": 1, "assists": 2, "saves": 3, "shots": 4, "mvp": true } }
		}`
		var p ReplayPlayer
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		if p.Score != 450 || p.Goals != 1 || p.Assists != 2 || p.Saves != 3 || p.Shots != 4 || !p.MVP {
			t.Errorf("flat fields not hydrated from nested Stats.Core")
		}
	})

	t.Run("legitimate 0-score player with 0 nested core stats", func(t *testing.T) {
		payload := `{
			"name": "AFKPlayer", "score": 0, "goals": 0, "assists": 0, "saves": 0, "shots": 0,
			"stats": { "core": { "score": 0, "goals": 0, "assists": 0, "saves": 0, "shots": 0, "mvp": false } }
		}`
		var p ReplayPlayer
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		if p.Score != 0 || p.Goals != 0 || p.Assists != 0 || p.Saves != 0 || p.Shots != 0 || p.Core == nil || p.Core.Score != 0 {
			t.Errorf("zero stats clobbered or nil p.Core: %+v", p)
		}
	})

	t.Run("partial zero and non-zero fields preserved accurately", func(t *testing.T) {
		payload := `{"name":"DefenderOnly","score":150,"goals":0,"assists":0,"saves":3,"shots":0,"demos":{"inflicted":0,"taken":2}}`
		var p ReplayPlayer
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		if p.Score != 150 || p.Goals != 0 || p.Saves != 3 || p.Core == nil || p.Core.Score != 150 || p.Demo == nil || p.Demo.Taken != 2 {
			t.Errorf("partial stats or synthesis mismatch: %+v", p)
		}
	})
}

func TestStress_MemoryAndAllocation_NoLeaks(t *testing.T) {
	raw := []byte(canonicalNestedFixture)
	for i := 0; i < 100; i++ {
		var d ReplayDetail
		_ = json.Unmarshal(raw, &d)
	}

	runtime.GC()
	var mStart runtime.MemStats
	runtime.ReadMemStats(&mStart)

	const iterations = 5000
	for i := 0; i < iterations; i++ {
		var d ReplayDetail
		if err := json.Unmarshal(raw, &d); err != nil || d.Blue.Players[0].Score != 650 {
			t.Fatalf("unmarshal failure or corruption at %d: %v", i, err)
		}
	}

	runtime.GC()
	var mEnd runtime.MemStats
	runtime.ReadMemStats(&mEnd)

	if heapGrowth := int64(mEnd.HeapInuse) - int64(mStart.HeapInuse); heapGrowth > 32*1024*1024 {
		t.Fatalf("suspected memory leak: heap inuse grew by %d bytes", heapGrowth)
	}
}

func TestStress_Concurrency_RaceDetection(t *testing.T) {
	rawNested := []byte(canonicalNestedFixture)
	flatPayload := []byte(`{"id":"concurrent-flat","replay_title":"Concur Match","blue":{"players":[{"name":"ConcurP1","score":300,"goals":1}]}}`)

	const numGoroutines = 100
	const perGoroutineIterations = 50
	var wg sync.WaitGroup
	startBarrier := make(chan struct{})

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			<-startBarrier
			r := rand.New(rand.NewSource(int64(gid)))
			for i := 0; i < perGoroutineIterations; i++ {
				var d ReplayDetail
				if r.Intn(2) == 0 {
					if err := json.Unmarshal(rawNested, &d); err != nil || d.ID != "full-fixture-abc-123" || d.Blue.Players[0].Score != 650 {
						t.Errorf("goroutine %d nested mismatch: %v", gid, err)
						return
					}
				} else {
					if err := json.Unmarshal(flatPayload, &d); err != nil || d.Title != "Concur Match" || d.Blue.Players[0].Score != 300 {
						t.Errorf("goroutine %d flat mismatch: %v", gid, err)
						return
					}
				}
				_ = MapPlaylistSlugToID(fmt.Sprintf("%d", r.Intn(50)))
			}
		}(g)
	}

	close(startBarrier)
	wg.Wait()
}
