package main

import "testing"

func TestTranscriptStart(t *testing.T) {
	m := model{}
	_, cmd := m.Update(fixtureAction("g1-start"))
	if cmd == nil {
		t.Fatal("G1 start did not schedule a stock transcript insertion")
	}
}

func TestTranscriptReadyPrefix(t *testing.T) {
	tr := transcript{blocks: []string{"G1[0000]", "G1[0001]", "G1[0002]"}, ready: []bool{false, false, true}}
	if tr.schedule() != nil {
		t.Fatal("out-of-order completion emitted")
	}
	tr.ready[0] = true
	if tr.schedule() == nil || tr.schedule() != nil {
		t.Fatal("one outstanding insertion invariant failed")
	}
	if tr.observe(commitWrite{IDs: []string{"G1[0000]"}, N: 8, Requested: 8}) != nil || tr.confirmed != 1 {
		t.Fatal("stable prefix passed a missing block")
	}
	tr.ready[1] = true
	if tr.schedule() == nil {
		t.Fatal("ready prefix not scheduled")
	}
	if tr.observe(commitWrite{IDs: []string{"G1[0001]"}, N: 8, Requested: 8}) == nil {
		t.Fatal("next contiguous block not scheduled")
	}
	if tr.observe(commitWrite{IDs: []string{"G1[0002]"}, N: 8, Requested: 8}) != nil || tr.confirmed != 3 {
		t.Fatal("stream did not finish cleanly")
	}
}

func TestManualTranscriptStart(t *testing.T) {
	m := model{g1: true, manualTranscript: true, transcriptLines: 2000}
	m2, cmd := m.Update(fixtureAction("g1-manual"))
	got := m2.(model)
	if cmd == nil || len(got.transcript.blocks) != 2000 || got.transcript.scheduled != 1 {
		t.Fatalf("manual transcript did not start: %+v", got.transcript)
	}
}
