package distillcmd

import (
	"testing"
)

// Tests ported from ~/trace-tools/trace-normalize.test.js. The single failing
// JS test ("isUserActionEvent: rejects serverStatus component var") is not
// ported because the JS source it asserts against does not actually behave
// that way — see the comment in IsUserActionEvent.

func ev(fields ...interface{}) Event {
	if len(fields)%2 != 0 {
		panic("ev: fields must come in key/value pairs")
	}
	out := Event{}
	for i := 0; i < len(fields); i += 2 {
		out[fields[i].(string)] = fields[i+1]
	}
	return out
}

func diff(paths ...string) []interface{} {
	out := make([]interface{}, len(paths))
	for i, p := range paths {
		out[i] = map[string]interface{}{"path": p}
	}
	return out
}

func TestIsPollingEvent(t *testing.T) {
	cases := []struct {
		name string
		in   Event
		want bool
	}{
		{"serverInfo state change", ev("kind", "state:changes", "eventName", "DataSource:serverInfo"), true},
		{"status api:start", ev("kind", "api:start", "url", "/api/status"), true},
		{"status api:complete", ev("kind", "api:complete", "url", "/api/status"), true},
		{"license api:start", ev("kind", "api:start", "url", "/api/license"), true},
		{"loaded handler for serverInfo", ev("kind", "handler:start", "eventName", "loaded", "componentLabel", "serverInfo"), true},
		{"AppState stats polling", ev("kind", "state:changes", "eventName", "AppState:main", "diffJson", diff("stats.cpu", "status")), true},
		{"rejects user API call", ev("kind", "api:start", "url", "/api/users"), false},
		{"rejects non-polling state change", ev("kind", "state:changes", "eventName", "AppState:main", "diffJson", diff("users")), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsPollingEvent(c.in); got != c.want {
				t.Errorf("IsPollingEvent = %v, want %v", got, c.want)
			}
		})
	}
}

func TestIsUserActionEvent(t *testing.T) {
	cases := []struct {
		name string
		in   Event
		want bool
	}{
		{"API call to users", ev("kind", "api:start", "url", "/api/users"), true},
		{"rejects status API", ev("kind", "api:start", "url", "/api/status"), false},
		{"rejects license API", ev("kind", "api:complete", "url", "/api/license"), false},
		{"user state change", ev("kind", "state:changes", "eventName", "DataSource:users", "diffJson", diff("items")), true},
		{"rejects serverInfo", ev("kind", "state:changes", "eventName", "DataSource:serverInfo"), false},
		{"component vars change", ev("kind", "component:vars:change", "diff", diff("items")), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsUserActionEvent(c.in); got != c.want {
				t.Errorf("IsUserActionEvent = %v, want %v", got, c.want)
			}
		})
	}
}

func TestIsOrphanedPollingEvent(t *testing.T) {
	cases := []struct {
		name string
		in   Event
		want bool
	}{
		{"loaded handler:start", ev("kind", "handler:start", "eventName", "loaded"), true},
		{"loaded handler:complete", ev("kind", "handler:complete", "eventName", "loaded"), true},
		{"rejects click handler", ev("kind", "handler:start", "eventName", "click"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsOrphanedPollingEvent(c.in); got != c.want {
				t.Errorf("IsOrphanedPollingEvent = %v, want %v", got, c.want)
			}
		})
	}
}

func TestDefaultSortKey(t *testing.T) {
	if got := DefaultSortKey(ev("perfTs", 100.0, "ts", 50.0)); got != 100.0 {
		t.Errorf("prefers perfTs: got %v", got)
	}
	if got := DefaultSortKey(ev("ts", 50.0)); got != 50.0 {
		t.Errorf("falls back to ts: got %v", got)
	}
	if got := DefaultSortKey(ev()); got != 0 {
		t.Errorf("empty: got %v", got)
	}
	if got := DefaultSortKey(nil); got != 0 {
		t.Errorf("nil: got %v", got)
	}
}

func TestMatchAPIPairs_BasicMatch(t *testing.T) {
	ResetRequestIDCounter()
	entries := []Event{
		ev("kind", "api:start", "method", "GET", "url", "/api/users", "instanceId", "ds1", "perfTs", 100.0, "traceId", "i-1"),
		ev("kind", "api:complete", "method", "GET", "url", "/api/users", "instanceId", "ds1", "perfTs", 200.0),
	}
	MatchAPIPairs(entries)
	if entries[0]["_requestId"] != entries[1]["_requestId"] {
		t.Errorf("requestId mismatch: %v vs %v", entries[0]["_requestId"], entries[1]["_requestId"])
	}
	if entries[1]["traceId"] != "i-1" {
		t.Errorf("traceId not inherited: got %v", entries[1]["traceId"])
	}
}

func TestMatchAPIPairs_MultiplePairs(t *testing.T) {
	ResetRequestIDCounter()
	entries := []Event{
		ev("kind", "api:start", "method", "GET", "url", "/api/users", "instanceId", "ds1", "perfTs", 100.0, "traceId", "i-1"),
		ev("kind", "api:start", "method", "GET", "url", "/api/users", "instanceId", "ds1", "perfTs", 150.0, "traceId", "i-2"),
		ev("kind", "api:complete", "method", "GET", "url", "/api/users", "instanceId", "ds1", "perfTs", 200.0),
		ev("kind", "api:complete", "method", "GET", "url", "/api/users", "instanceId", "ds1", "perfTs", 250.0),
	}
	MatchAPIPairs(entries)
	// First completion (200) matches most recent start before it (150 = i-2).
	// Second completion (250) matches remaining start (100 = i-1).
	if entries[2]["_requestId"] != entries[1]["_requestId"] {
		t.Errorf("first complete should match second start: %v vs %v", entries[2]["_requestId"], entries[1]["_requestId"])
	}
	if entries[3]["_requestId"] != entries[0]["_requestId"] {
		t.Errorf("second complete should match first start: %v vs %v", entries[3]["_requestId"], entries[0]["_requestId"])
	}
	if entries[2]["traceId"] != "i-2" {
		t.Errorf("first complete traceId: got %v", entries[2]["traceId"])
	}
	if entries[3]["traceId"] != "i-1" {
		t.Errorf("second complete traceId: got %v", entries[3]["traceId"])
	}
}

func TestMatchAPIPairs_DifferentInstanceIds(t *testing.T) {
	ResetRequestIDCounter()
	entries := []Event{
		ev("kind", "api:start", "method", "GET", "url", "/api/users", "instanceId", "ds1", "perfTs", 100.0),
		ev("kind", "api:complete", "method", "GET", "url", "/api/users", "instanceId", "ds2", "perfTs", 200.0),
	}
	MatchAPIPairs(entries)
	if rid, _ := entries[0]["_requestId"].(string); rid == "" {
		t.Errorf("api:start should have _requestId")
	}
	if _, ok := entries[1]["_requestId"]; ok {
		t.Errorf("api:complete should NOT have _requestId (no match): got %v", entries[1]["_requestId"])
	}
}

func TestGroupByTraceID(t *testing.T) {
	entries := []Event{
		ev("kind", "handler:start", "traceId", "i-1"),
		ev("kind", "state:changes", "traceId", "i-1"),
		ev("kind", "api:start"),
		ev("kind", "handler:start", "traceId", "startup-abc"),
	}
	tracesMap, orphans := GroupByTraceID(entries)
	if got := len(tracesMap.Get("i-1")); got != 2 {
		t.Errorf("i-1 length: got %v", got)
	}
	if got := len(tracesMap.Get("startup-abc")); got != 1 {
		t.Errorf("startup-abc length: got %v", got)
	}
	if got := len(orphans); got != 1 {
		t.Errorf("orphans length: got %v", got)
	}
	if eventString(orphans[0], "kind") != "api:start" {
		t.Errorf("orphan kind: got %v", orphans[0]["kind"])
	}
}

func TestMergeBootstrapOrphans(t *testing.T) {
	tm := NewTracesMap()
	tm.Set("startup-abc", []Event{ev("kind", "handler:start", "perfTs", 100.0)})
	orphans := []Event{
		ev("kind", "state:changes", "perfTs", 50.0), // before startup
		ev("kind", "state:changes", "perfTs", 0.0),   // no timestamp
		ev("kind", "api:start", "perfTs", 500.0),     // after startup
	}
	remaining := MergeBootstrapOrphans(tm, orphans, nil)
	if got := len(tm.Get("startup-abc")); got != 3 {
		t.Errorf("startup length: got %v, want 3", got)
	}
	if got := len(remaining); got != 1 {
		t.Errorf("remaining length: got %v, want 1", got)
	}
	if got, _ := eventNumber(remaining[0], "perfTs"); got != 500.0 {
		t.Errorf("remaining perfTs: got %v", got)
	}
}

func TestMergePollingTraces_AllLoadedHandlers(t *testing.T) {
	tm := NewTracesMap()
	tm.Set("startup-abc", []Event{ev("kind", "handler:start", "perfTs", 10.0)})
	tm.Set("t-poll", []Event{
		ev("kind", "handler:start", "eventName", "loaded"),
		ev("kind", "handler:complete", "eventName", "loaded"),
		ev("kind", "state:changes"),
	})
	tm.Set("i-click", []Event{ev("kind", "handler:start", "eventName", "click")})

	MergePollingTraces(tm)

	if tm.Has("t-poll") {
		t.Errorf("t-poll should be merged away")
	}
	if !tm.Has("i-click") {
		t.Errorf("i-click should be kept")
	}
	if got := len(tm.Get("startup-abc")); got != 4 {
		t.Errorf("startup length: got %v, want 4", got)
	}
}

func TestMergePollingTraces_StateOnlyMethodCalls(t *testing.T) {
	tm := NewTracesMap()
	tm.Set("startup-abc", []Event{ev("kind", "handler:start", "perfTs", 10.0)})
	tm.Set("t-state", []Event{
		ev("kind", "method:call", "componentLabel", "state", "displayLabel", "state.update({...})"),
	})
	tm.Set("t-mixed", []Event{
		ev("kind", "method:call", "componentLabel", "state"),
		ev("kind", "handler:start", "eventName", "click"),
	})

	MergePollingTraces(tm)

	if tm.Has("t-state") {
		t.Errorf("t-state should be merged")
	}
	if !tm.Has("t-mixed") {
		t.Errorf("t-mixed should be kept")
	}
	if got := len(tm.Get("startup-abc")); got != 2 {
		t.Errorf("startup length: got %v, want 2", got)
	}
}

func TestMergePollingTraces_KeepsNativeEvents(t *testing.T) {
	tm := NewTracesMap()
	tm.Set("startup-abc", []Event{})
	tm.Set("t-native", []Event{
		ev("kind", "handler:start", "eventName", "loaded"),
		ev("kind", "native:click"),
	})

	MergePollingTraces(tm)

	if !tm.Has("t-native") {
		t.Errorf("t-native should be kept (has native event)")
	}
}

func TestMergeChangeListenerOrphans(t *testing.T) {
	tm := NewTracesMap()
	tm.Set("t-cl", []Event{
		ev("kind", "handler:start", "componentType", "ChangeListener", "perfTs", 300.0),
		ev("kind", "handler:complete", "perfTs", 400.0),
	})
	orphans := []Event{
		ev("kind", "api:start", "instanceId", "ds1", "perfTs", 180.0, "url", "/api/users"),
		ev("kind", "api:complete", "instanceId", "ds1", "perfTs", 250.0, "url", "/api/users"),
		ev("kind", "state:changes", "perfTs", 500.0), // unrelated
	}
	remaining := MergeChangeListenerOrphans(tm, orphans)

	if got := len(tm.Get("t-cl")); got != 4 {
		t.Errorf("t-cl length: got %v, want 4", got)
	}
	if got := len(remaining); got != 1 {
		t.Errorf("remaining length: got %v, want 1", got)
	}
	if got, _ := eventNumber(remaining[0], "perfTs"); got != 500.0 {
		t.Errorf("remaining perfTs: got %v", got)
	}
}

func TestRehomeByTimeWindow_Orphans(t *testing.T) {
	tm := NewTracesMap()
	tm.Set("i-1", []Event{
		ev("kind", "handler:start", "perfTs", 100.0),
		ev("kind", "handler:complete", "perfTs", 300.0),
	})
	orphans := []Event{
		ev("kind", "api:start", "url", "/api/users", "perfTs", 200.0),                                                 // in window
		ev("kind", "state:changes", "eventName", "DataSource:users", "perfTs", 350.0, "diffJson", diff("items")),       // in window+buffer
		ev("kind", "api:start", "url", "/api/users", "perfTs", 900.0),                                                 // outside
	}
	remaining := RehomeByTimeWindow(tm, orphans, RehomeOptions{})

	if got := len(tm.Get("i-1")); got != 4 {
		t.Errorf("i-1 length: got %v, want 4", got)
	}
	if got := len(remaining); got != 1 {
		t.Errorf("remaining length: got %v, want 1", got)
	}
	if got, _ := eventNumber(remaining[0], "perfTs"); got != 900.0 {
		t.Errorf("remaining perfTs: got %v", got)
	}
}

func TestRehomeByTimeWindow_FromSourceTrace(t *testing.T) {
	tm := NewTracesMap()
	tm.Set("startup-abc", []Event{
		ev("kind", "handler:start", "perfTs", 10.0),
		ev("kind", "api:start", "url", "/api/users", "perfTs", 150.0),
	})
	tm.Set("i-1", []Event{
		ev("kind", "handler:start", "perfTs", 100.0),
		ev("kind", "handler:complete", "perfTs", 300.0),
	})
	RehomeByTimeWindow(tm, nil, RehomeOptions{SourceTraceID: "startup-abc"})

	if got := len(tm.Get("startup-abc")); got != 1 {
		t.Errorf("startup length: got %v, want 1", got)
	}
	if got := len(tm.Get("i-1")); got != 3 {
		t.Errorf("i-1 length: got %v, want 3", got)
	}
}

func TestMergeOrphanedPollingToStartup(t *testing.T) {
	tm := NewTracesMap()
	tm.Set("startup-abc", []Event{})
	orphans := []Event{
		ev("kind", "handler:start", "eventName", "loaded"),                  // polling
		ev("kind", "api:start", "url", "/api/status"),                       // polling
		ev("kind", "state:changes", "eventName", "DataSource:serverInfo"),   // polling
		ev("kind", "api:start", "url", "/api/users"),                        // not polling
	}
	remaining := MergeOrphanedPollingToStartup(tm, orphans)

	if got := len(tm.Get("startup-abc")); got != 3 {
		t.Errorf("startup length: got %v, want 3", got)
	}
	if got := len(remaining); got != 1 {
		t.Errorf("remaining length: got %v, want 1", got)
	}
	if got := eventString(remaining[0], "url"); got != "/api/users" {
		t.Errorf("remaining url: got %v", got)
	}
}

func TestRehomeOrphanedValueChanges(t *testing.T) {
	vc := ev("kind", "value:change", "perfTs", 105.0, "component", "TextBox")
	tg0 := &TraceGroup{TraceID: "unknown", Events: []Event{vc}, FirstPerfTs: 105.0, HasFirstPerf: true}
	tg1 := &TraceGroup{TraceID: "i-1", Events: []Event{ev("kind", "interaction", "perfTs", 100.0)}, FirstPerfTs: 100.0, HasFirstPerf: true}
	tg2 := &TraceGroup{TraceID: "i-2", Events: []Event{ev("kind", "interaction", "perfTs", 500.0)}, FirstPerfTs: 500.0, HasFirstPerf: true}
	traces := []*TraceGroup{tg0, tg1, tg2}

	RehomeOrphanedValueChanges(traces, func(e Event) float64 {
		v, _ := eventNumber(e, "perfTs")
		return v
	})

	if got := len(tg1.Events); got != 2 {
		t.Errorf("i-1 length: got %v, want 2", got)
	}
	if got := len(tg0.Events); got != 0 {
		t.Errorf("source length: got %v, want 0", got)
	}
}

func TestFilterPollingFromInteractions(t *testing.T) {
	tm := NewTracesMap()
	tm.Set("i-1", []Event{
		ev("kind", "handler:start", "eventName", "click"),
		ev("kind", "state:changes", "eventName", "DataSource:serverInfo"), // polling
		ev("kind", "api:start", "url", "/api/status"),                     // polling
		ev("kind", "api:complete", "url", "/api/users"),                   // not polling
	})
	tm.Set("startup-abc", []Event{
		ev("kind", "state:changes", "eventName", "DataSource:serverInfo"), // kept in startup
	})
	FilterPollingFromInteractions(tm)

	if got := len(tm.Get("i-1")); got != 2 {
		t.Errorf("i-1 length: got %v, want 2", got)
	}
	if got := len(tm.Get("startup-abc")); got != 1 {
		t.Errorf("startup length: got %v, want 1", got)
	}
}

func TestCoalesceValueChanges(t *testing.T) {
	events := []Event{
		ev("kind", "value:change", "component", "TextBox1", "displayLabel", "a"),
		ev("kind", "value:change", "component", "TextBox1", "displayLabel", "ab"),
		ev("kind", "value:change", "component", "TextBox1", "displayLabel", "abc"),
		ev("kind", "value:change", "component", "Slider1", "displayLabel", "50"),
		ev("kind", "handler:start"), // ignored
	}
	out := CoalesceValueChanges(events)
	if got := len(out); got != 2 {
		t.Fatalf("length: got %v, want 2", got)
	}
	for _, e := range out {
		switch eventString(e, "component") {
		case "TextBox1":
			if got := eventString(e, "displayLabel"); got != "abc" {
				t.Errorf("TextBox1 last value: got %v", got)
			}
		case "Slider1":
			if got := eventString(e, "displayLabel"); got != "50" {
				t.Errorf("Slider1 value: got %v", got)
			}
		default:
			t.Errorf("unexpected component: %v", eventString(e, "component"))
		}
	}
}

func TestDedupByFingerprint(t *testing.T) {
	events := []Event{
		ev("kind", "api:start", "method", "GET", "url", "/api/status"),
		ev("kind", "api:start", "method", "GET", "url", "/api/status"),
		ev("kind", "api:start", "method", "GET", "url", "/api/status"),
		ev("kind", "api:start", "method", "POST", "url", "/api/users"),
	}
	res := DedupByFingerprint(events, func(e Event) (string, bool) {
		return eventString(e, "method") + "|" + eventString(e, "url"), true
	})
	if got := len(res.Unique); got != 2 {
		t.Errorf("unique count: got %v, want 2", got)
	}
	if res.DedupedCount != 2 {
		t.Errorf("deduped count: got %v, want 2", res.DedupedCount)
	}
	for _, u := range res.Unique {
		if eventString(u.Event, "url") == "/api/status" && u.Count != 3 {
			t.Errorf("/api/status count: got %v, want 3", u.Count)
		}
	}
}

func TestDedupByFingerprint_SkipsNullKeys(t *testing.T) {
	events := []Event{
		ev("kind", "api:start", "url", "/a"),
		ev("kind", "handler:start"),
	}
	res := DedupByFingerprint(events, func(e Event) (string, bool) {
		url := eventString(e, "url")
		if url == "" {
			return "", false
		}
		return url, true
	})
	if got := len(res.Unique); got != 1 {
		t.Errorf("unique count: got %v, want 1", got)
	}
}

func TestPreprocessTraces_FullPipeline(t *testing.T) {
	ResetRequestIDCounter()
	entries := []Event{
		// Startup trace
		ev("kind", "handler:start", "traceId", "startup-1", "perfTs", 10.0),
		ev("kind", "handler:complete", "traceId", "startup-1", "perfTs", 20.0),
		// Bootstrap orphan (before startup)
		ev("kind", "state:changes", "perfTs", 5.0, "eventName", "init"),
		// Interaction trace
		ev("kind", "handler:start", "traceId", "i-1", "perfTs", 100.0),
		ev("kind", "handler:complete", "traceId", "i-1", "perfTs", 300.0),
		// Polling in interaction (filtered)
		ev("kind", "state:changes", "traceId", "i-1", "perfTs", 150.0, "eventName", "DataSource:serverInfo"),
		// API pair
		ev("kind", "api:start", "traceId", "i-1", "method", "PUT", "url", "/api/users", "instanceId", "ds1", "perfTs", 120.0),
		ev("kind", "api:complete", "method", "PUT", "url", "/api/users", "instanceId", "ds1", "perfTs", 250.0),
		// Polling-only trace
		ev("kind", "handler:start", "traceId", "t-poll", "eventName", "loaded", "perfTs", 50.0),
		ev("kind", "handler:complete", "traceId", "t-poll", "eventName", "loaded", "perfTs", 60.0),
	}

	res := PreprocessTraces(entries, nil)

	if res.TracesMap.Has("t-poll") {
		t.Errorf("t-poll should be merged away")
	}
	startup := res.TracesMap.Get("startup-1")
	if startup == nil {
		t.Fatalf("startup-1 missing")
	}
	hasBootstrap := false
	for _, e := range startup {
		if ts, _ := eventNumber(e, "perfTs"); ts == 5.0 {
			hasBootstrap = true
		}
	}
	if !hasBootstrap {
		t.Errorf("bootstrap orphan not merged into startup")
	}

	interaction := res.TracesMap.Get("i-1")
	if interaction == nil {
		t.Fatalf("i-1 missing")
	}
	for _, e := range interaction {
		if eventString(e, "eventName") == "DataSource:serverInfo" {
			t.Errorf("polling event not filtered from i-1")
		}
	}
	hasAPIComplete := false
	for _, e := range interaction {
		if eventString(e, "kind") == "api:complete" && eventString(e, "url") == "/api/users" {
			hasAPIComplete = true
		}
	}
	if !hasAPIComplete {
		t.Errorf("api:complete not rehomed to i-1")
	}

	// API pair was matched
	var apiStart, apiComplete Event
	for _, e := range entries {
		if eventString(e, "kind") == "api:start" && eventString(e, "url") == "/api/users" {
			apiStart = e
		}
		if eventString(e, "kind") == "api:complete" && eventString(e, "url") == "/api/users" {
			apiComplete = e
		}
	}
	if apiStart["_requestId"] != apiComplete["_requestId"] {
		t.Errorf("api pair not matched: %v vs %v", apiStart["_requestId"], apiComplete["_requestId"])
	}
}
