package distillcmd

// Port of trace-tools/trace-normalize.js. Operates on raw log entries
// (the common denominator between the viewer and the distiller).
// Centralizes event classification, API pair matching, trace grouping,
// orphan re-homing, polling detection/filtering, and value coalescing.

import (
	"math"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
)

// Event is one raw log entry. JS treats events as free-form objects, and the
// distiller reads many ad-hoc fields, so we keep the structure open.
type Event = map[string]interface{}

// TracesMap preserves insertion order, matching JS Map semantics. JS
// findOrCreateStartupTraceId / mergePollingTraces / rehomeByTimeWindow
// iterate in insertion order; Go's built-in map does not.
type TracesMap struct {
	order []string
	data  map[string][]Event
}

func NewTracesMap() *TracesMap {
	return &TracesMap{data: map[string][]Event{}}
}

func (t *TracesMap) Has(id string) bool {
	_, ok := t.data[id]
	return ok
}

func (t *TracesMap) Get(id string) []Event {
	return t.data[id]
}

func (t *TracesMap) Set(id string, events []Event) {
	if _, ok := t.data[id]; !ok {
		t.order = append(t.order, id)
	}
	t.data[id] = events
}

func (t *TracesMap) Append(id string, e Event) {
	if _, ok := t.data[id]; !ok {
		t.order = append(t.order, id)
	}
	t.data[id] = append(t.data[id], e)
}

func (t *TracesMap) Delete(id string) {
	if _, ok := t.data[id]; !ok {
		return
	}
	delete(t.data, id)
	for i, k := range t.order {
		if k == id {
			t.order = append(t.order[:i], t.order[i+1:]...)
			break
		}
	}
}

func (t *TracesMap) Keys() []string {
	out := make([]string, len(t.order))
	copy(out, t.order)
	return out
}

// ForEach iterates entries in insertion order. The callback may not mutate
// the map during iteration except via the returned closure operations.
func (t *TracesMap) ForEach(fn func(id string, events []Event)) {
	for _, id := range t.Keys() {
		fn(id, t.data[id])
	}
}

// Size returns the number of trace groups.
func (t *TracesMap) Size() int {
	return len(t.data)
}

// ---------------------------------------------------------------------------
// Field accessors — JS reads dozens of optional fields off events. Centralize
// the type assertions so call sites stay readable.
// ---------------------------------------------------------------------------

func eventString(e Event, key string) string {
	v, _ := e[key].(string)
	return v
}

func eventNumber(e Event, key string) (float64, bool) {
	switch v := e[key].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}

func eventArray(e Event, key string) []interface{} {
	v, _ := e[key].([]interface{})
	return v
}

// ---------------------------------------------------------------------------
// Predicates — reusable event classification
// ---------------------------------------------------------------------------

// IsPollingEvent returns true for events that are polling noise (status checks,
// serverInfo, etc.).
func IsPollingEvent(e Event) bool {
	kind := eventString(e, "kind")
	eventName := eventString(e, "eventName")
	url := eventString(e, "url")
	componentLabel := eventString(e, "componentLabel")

	if kind == "state:changes" && eventName == "DataSource:serverInfo" {
		return true
	}
	if (kind == "api:start" || kind == "api:complete") && url != "" &&
		(strings.Contains(url, "/status") || strings.Contains(url, "/license")) {
		return true
	}
	if (kind == "handler:start" || kind == "handler:complete") &&
		eventName == "loaded" && componentLabel == "serverInfo" {
		return true
	}
	if kind == "state:changes" && strings.HasPrefix(eventName, "AppState:") {
		return allDiffPathsArePolling(eventArray(e, "diffJson"))
	}
	return false
}

// IsUserActionEvent returns true for events that represent user-triggered
// actions (not polling). Used to decide which orphaned events should be
// re-homed to interaction traces.
func IsUserActionEvent(e Event) bool {
	kind := eventString(e, "kind")
	eventName := eventString(e, "eventName")
	url := eventString(e, "url")

	if kind == "api:start" || kind == "api:complete" || kind == "api:error" {
		if url != "" && (strings.Contains(url, "/status") || strings.Contains(url, "/license")) {
			return false
		}
		return true
	}
	if kind == "state:changes" {
		if eventName == "DataSource:serverInfo" {
			return false
		}
		if strings.HasPrefix(eventName, "AppState:") {
			if allDiffPathsArePolling(eventArray(e, "diffJson")) {
				return false
			}
		}
		return true
	}
	// component:vars:change is unconditionally true in the JS source. Note that
	// trace-normalize.test.js has one test case ("rejects serverStatus
	// component var") that asserts the opposite — that test fails against the
	// JS source today and is not ported.
	if kind == "component:vars:change" {
		return true
	}
	if kind == "data:bind" {
		return true
	}
	return false
}

// IsOrphanedPollingEvent returns true for events that look like orphaned
// polling (should merge into startup).
func IsOrphanedPollingEvent(e Event) bool {
	kind := eventString(e, "kind")
	eventName := eventString(e, "eventName")
	url := eventString(e, "url")

	if (kind == "handler:start" || kind == "handler:complete") && eventName == "loaded" {
		return true
	}
	if (kind == "api:start" || kind == "api:complete") && strings.Contains(url, "/status") {
		return true
	}
	if kind == "state:changes" && eventName == "DataSource:serverInfo" {
		return true
	}
	if kind == "state:changes" && strings.HasPrefix(eventName, "AppState:") {
		return allDiffPathsArePolling(eventArray(e, "diffJson"))
	}
	return false
}

// allDiffPathsArePolling implements the .every() check in the JS predicates
// for AppState diffs. Returns false on empty input (matching JS Array.every,
// but the JS callers only enter this branch when diffJson is truthy).
func allDiffPathsArePolling(diff []interface{}) bool {
	if len(diff) == 0 {
		return false
	}
	for _, d := range diff {
		dm, ok := d.(map[string]interface{})
		if !ok {
			return false
		}
		path, _ := dm["path"].(string)
		switch {
		case path == "stats":
		case strings.HasPrefix(path, "stats."):
		case path == "status":
		case path == "logs":
		case path == "sessions":
		default:
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Timestamp helpers
// ---------------------------------------------------------------------------

// SortKeyFn extracts a numeric sort key from an event.
type SortKeyFn func(Event) float64

// DefaultSortKey prefers perfTs, falls back to ts, then 0.
func DefaultSortKey(e Event) float64 {
	if e == nil {
		return 0
	}
	if v, ok := eventNumber(e, "perfTs"); ok {
		return v
	}
	if v, ok := eventNumber(e, "ts"); ok {
		return v
	}
	return 0
}

// ---------------------------------------------------------------------------
// API pair matching
// ---------------------------------------------------------------------------

var requestIDCounter int64

// ResetRequestIDCounter is exported for testing only.
func ResetRequestIDCounter() {
	atomic.StoreInt64(&requestIDCounter, 0)
}

func nextRequestID() string {
	n := atomic.AddInt64(&requestIDCounter, 1)
	return "req-" + itoa(n)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

type pendingRequest struct {
	requestID string
	perfTs    float64
	hasPerfTs bool
	method    string
	url       string
	traceID   string
}

// MatchAPIPairs matches api:complete/api:error events to their api:start by
// method+url+timing, assigns _requestId to both sides, and inherits traceId
// from the matched start. Mutates entries in place.
func MatchAPIPairs(entries []Event) {
	pendingByInstance := map[string][]*pendingRequest{}

	for _, e := range entries {
		if eventString(e, "kind") != "api:start" {
			continue
		}
		rid, _ := e["_requestId"].(string)
		if rid == "" {
			rid = nextRequestID()
			e["_requestId"] = rid
		}
		key := eventString(e, "instanceId")
		if key == "" {
			key = "unknown"
		}
		method := strings.ToUpper(eventString(e, "method"))
		if method == "" {
			method = "GET"
		}
		perfTs, hasPerfTs := eventNumber(e, "perfTs")
		pr := &pendingRequest{
			requestID: rid,
			perfTs:    perfTs,
			hasPerfTs: hasPerfTs,
			method:    method,
			url:       eventString(e, "url"),
			traceID:   eventString(e, "traceId"),
		}
		pendingByInstance[key] = append(pendingByInstance[key], pr)
	}

	type completion struct {
		index int
		event Event
	}
	var completions []completion
	for i, e := range entries {
		kind := eventString(e, "kind")
		if kind != "api:complete" && kind != "api:error" {
			continue
		}
		if rid, _ := e["_requestId"].(string); rid != "" {
			continue
		}
		completions = append(completions, completion{index: i, event: e})
	}
	sort.SliceStable(completions, func(a, b int) bool {
		ta, _ := eventNumber(completions[a].event, "perfTs")
		tb, _ := eventNumber(completions[b].event, "perfTs")
		return ta < tb
	})

	for _, c := range completions {
		e := c.event
		key := eventString(e, "instanceId")
		if key == "" {
			key = "unknown"
		}
		queue := pendingByInstance[key]
		if len(queue) == 0 {
			continue
		}

		method := strings.ToUpper(eventString(e, "method"))
		if method == "" {
			method = "GET"
		}
		completePerfTs, hasComplete := eventNumber(e, "perfTs")
		if !hasComplete {
			completePerfTs = math.Inf(1)
		}
		completeTraceID := eventString(e, "traceId")
		url := eventString(e, "url")

		bestIdx := -1
		bestTs := math.Inf(-1)
		for idx, r := range queue {
			if r.method == method && r.url == url {
				if completeTraceID != "" && r.traceID != "" && r.traceID != completeTraceID {
					continue
				}
				if r.hasPerfTs && r.perfTs <= completePerfTs && r.perfTs > bestTs {
					bestIdx = idx
					bestTs = r.perfTs
				}
			}
		}

		if bestIdx == -1 {
			fallbackIdx := -1
			fallbackTs := math.Inf(-1)
			for idx, r := range queue {
				ok := completeTraceID == "" || r.traceID == "" || r.traceID == completeTraceID
				if ok && r.hasPerfTs && r.perfTs > fallbackTs {
					fallbackIdx = idx
					fallbackTs = r.perfTs
				}
			}
			if fallbackIdx == -1 {
				fallbackIdx = 0
			}
			bestIdx = fallbackIdx
		}

		matched := queue[bestIdx]
		pendingByInstance[key] = append(queue[:bestIdx], queue[bestIdx+1:]...)
		e["_requestId"] = matched.requestID
		if matched.traceID != "" {
			e["traceId"] = matched.traceID
		} else {
			delete(e, "traceId")
		}
	}
}

// ---------------------------------------------------------------------------
// Trace grouping
// ---------------------------------------------------------------------------

// GroupByTraceID groups entries by traceId. Entries without traceId are
// returned as orphans.
func GroupByTraceID(entries []Event) (*TracesMap, []Event) {
	tracesMap := NewTracesMap()
	var orphans []Event
	for _, e := range entries {
		traceID := eventString(e, "traceId")
		if traceID == "" {
			orphans = append(orphans, e)
			continue
		}
		tracesMap.Append(traceID, e)
	}
	return tracesMap, orphans
}

// FindOrCreateStartupTraceID finds the startup trace ID in tracesMap,
// creating a synthetic one if missing.
func FindOrCreateStartupTraceID(t *TracesMap) string {
	for _, k := range t.Keys() {
		if strings.HasPrefix(k, "startup-") {
			return k
		}
	}
	id := "startup-synthetic"
	t.Set(id, []Event{})
	return id
}

// ---------------------------------------------------------------------------
// Orphan merging / re-homing
// ---------------------------------------------------------------------------

// MergeBootstrapOrphans merges orphans before/at startup into the startup
// trace. Returns the remaining (non-bootstrap) orphans.
func MergeBootstrapOrphans(t *TracesMap, orphans []Event, sortKey SortKeyFn) []Event {
	if sortKey == nil {
		sortKey = DefaultSortKey
	}
	startupID := FindOrCreateStartupTraceID(t)
	startup := t.Get(startupID)

	startupMin := math.Inf(1)
	for _, e := range startup {
		ts := sortKey(e)
		if ts == 0 {
			ts = math.Inf(1)
		}
		if ts < startupMin {
			startupMin = ts
		}
	}

	var remaining []Event
	for _, e := range orphans {
		ts := sortKey(e)
		if ts == 0 || ts <= startupMin+100 {
			e["_bootstrap"] = true
			startup = append(startup, e)
		} else {
			remaining = append(remaining, e)
		}
	}
	t.Set(startupID, startup)
	return remaining
}

// MergePollingTraces identifies traces where ALL handlers are "loaded"
// (polling) with no native events, and merges their non-interaction events
// into startup. Also merges traces with only method:call on "state".
func MergePollingTraces(t *TracesMap) {
	startupID := FindOrCreateStartupTraceID(t)
	startup := t.Get(startupID)
	var toMerge []string

	t.ForEach(func(traceID string, entries []Event) {
		if traceID == startupID {
			return
		}
		var handlers []Event
		hasNative := false
		hasInteraction := false
		for _, e := range entries {
			kind := eventString(e, "kind")
			if kind == "handler:start" {
				handlers = append(handlers, e)
			}
			if strings.HasPrefix(kind, "native:") {
				hasNative = true
			}
			if kind == "interaction" {
				hasInteraction = true
			}
		}

		if len(handlers) > 0 && !hasNative {
			allLoaded := true
			for _, h := range handlers {
				if eventString(h, "eventName") != "loaded" {
					allLoaded = false
					break
				}
			}
			if allLoaded {
				toMerge = append(toMerge, traceID)
				return
			}
		}

		if len(handlers) == 0 && !hasNative && !hasInteraction && len(entries) > 0 {
			allMethodCalls := true
			for _, e := range entries {
				if eventString(e, "kind") != "method:call" || eventString(e, "componentLabel") != "state" {
					allMethodCalls = false
					break
				}
			}
			if allMethodCalls {
				toMerge = append(toMerge, traceID)
			}
		}
	})

	for _, id := range toMerge {
		for _, e := range t.Get(id) {
			if eventString(e, "kind") != "interaction" {
				startup = append(startup, e)
			}
		}
		t.Delete(id)
	}
	t.Set(startupID, startup)

	if startupID == "startup-synthetic" && len(startup) == 0 {
		t.Delete(startupID)
	}
}

// MergeChangeListenerOrphans merges orphaned API events into ChangeListener
// traces they triggered. When a DataSource refetch triggers a ChangeListener,
// the API events are orphaned while the ChangeListener gets a t- trace.
// Merges them by timing proximity (≤100ms).
func MergeChangeListenerOrphans(t *TracesMap, orphans []Event) []Event {
	type apiEvent struct {
		e          Event
		instanceID string
	}
	var orphanedAPI []apiEvent
	for _, e := range orphans {
		kind := eventString(e, "kind")
		instanceID := eventString(e, "instanceId")
		_, hasPerfTs := eventNumber(e, "perfTs")
		if (kind == "api:start" || kind == "api:complete" || kind == "api:error") &&
			instanceID != "" && hasPerfTs {
			orphanedAPI = append(orphanedAPI, apiEvent{e: e, instanceID: instanceID})
		}
	}
	if len(orphanedAPI) == 0 {
		return orphans
	}

	byInstance := map[string][]Event{}
	byInstanceOrder := []string{}
	for _, oa := range orphanedAPI {
		if _, ok := byInstance[oa.instanceID]; !ok {
			byInstanceOrder = append(byInstanceOrder, oa.instanceID)
		}
		byInstance[oa.instanceID] = append(byInstance[oa.instanceID], oa.e)
	}

	t.ForEach(func(traceID string, entries []Event) {
		if !strings.HasPrefix(traceID, "t-") {
			return
		}
		hasChangeListener := false
		for _, e := range entries {
			if eventString(e, "kind") == "handler:start" &&
				eventString(e, "componentType") == "ChangeListener" {
				hasChangeListener = true
				break
			}
		}
		if !hasChangeListener {
			return
		}

		traceMinTs := math.Inf(1)
		hasAnyTs := false
		for _, e := range entries {
			if v, ok := eventNumber(e, "perfTs"); ok {
				hasAnyTs = true
				if v < traceMinTs {
					traceMinTs = v
				}
			}
		}
		if !hasAnyTs {
			return
		}

		var consumedInstances []string
		for _, instanceID := range byInstanceOrder {
			apiEvents, ok := byInstance[instanceID]
			if !ok {
				continue
			}
			var apiComplete Event
			for _, e := range apiEvents {
				if eventString(e, "kind") == "api:complete" {
					apiComplete = e
					break
				}
			}
			if apiComplete == nil {
				continue
			}
			completeTs, _ := eventNumber(apiComplete, "perfTs")
			diff := traceMinTs - completeTs
			if diff >= 0 && diff <= 100 {
				for _, e := range apiEvents {
					e["_mergedFromOrphan"] = true
					entries = append(entries, e)
				}
				consumedInstances = append(consumedInstances, instanceID)
			}
		}
		for _, id := range consumedInstances {
			delete(byInstance, id)
		}
		t.Set(traceID, entries)
	})

	var remaining []Event
	for _, e := range orphans {
		if v, _ := e["_mergedFromOrphan"].(bool); v {
			continue
		}
		remaining = append(remaining, e)
	}
	return remaining
}

// RehomeOptions configures RehomeByTimeWindow. Buffer and Filter behave like
// the JS opts object; SourceTraceID is "" if no source trace should be
// drained.
type RehomeOptions struct {
	Buffer         float64
	Filter         func(Event) bool
	SourceTraceID  string
	bufferProvided bool
}

// RehomeByTimeWindow re-homes orphaned events into interaction traces whose
// handler execution window contains them.
func RehomeByTimeWindow(t *TracesMap, orphans []Event, opts RehomeOptions) []Event {
	buffer := 500.0
	if opts.bufferProvided {
		buffer = opts.Buffer
	}
	filter := opts.Filter
	if filter == nil {
		filter = IsUserActionEvent
	}

	type window struct {
		traceID string
		startTs float64
		endTs   float64
	}
	var windows []window
	t.ForEach(func(traceID string, entries []Event) {
		if !strings.HasPrefix(traceID, "i-") {
			return
		}
		var startMin = math.Inf(1)
		var endMax = math.Inf(-1)
		hasStart, hasEnd := false, false
		for _, e := range entries {
			ts, ok := eventNumber(e, "perfTs")
			if !ok {
				continue
			}
			switch eventString(e, "kind") {
			case "handler:start":
				hasStart = true
				if ts < startMin {
					startMin = ts
				}
			case "handler:complete":
				hasEnd = true
				if ts > endMax {
					endMax = ts
				}
			}
		}
		if !hasStart || !hasEnd {
			return
		}
		windows = append(windows, window{traceID: traceID, startTs: startMin, endTs: endMax})
	})
	if len(windows) == 0 {
		return orphans
	}

	if opts.SourceTraceID != "" && t.Has(opts.SourceTraceID) {
		source := t.Get(opts.SourceTraceID)
		var keep []Event
		for _, e := range source {
			ts, ok := eventNumber(e, "perfTs")
			if !ok || !filter(e) {
				keep = append(keep, e)
				continue
			}
			moved := false
			for _, w := range windows {
				if ts >= w.startTs && ts <= w.endTs+buffer {
					e["_movedFromStartup"] = true
					t.Set(w.traceID, append(t.Get(w.traceID), e))
					moved = true
					break
				}
			}
			if !moved {
				keep = append(keep, e)
			}
		}
		t.Set(opts.SourceTraceID, keep)
	}

	var remaining []Event
	for _, e := range orphans {
		ts, ok := eventNumber(e, "perfTs")
		if !ok || !filter(e) {
			remaining = append(remaining, e)
			continue
		}
		moved := false
		for _, w := range windows {
			if ts >= w.startTs && ts <= w.endTs+buffer {
				e["_mergedByTimeWindow"] = true
				t.Set(w.traceID, append(t.Get(w.traceID), e))
				moved = true
				break
			}
		}
		if !moved {
			remaining = append(remaining, e)
		}
	}
	return remaining
}

// MergeOrphanedPollingToStartup merges remaining orphaned polling events into
// the startup trace.
func MergeOrphanedPollingToStartup(t *TracesMap, orphans []Event) []Event {
	startupID := ""
	for _, k := range t.Keys() {
		if strings.HasPrefix(k, "startup-") {
			startupID = k
			break
		}
	}
	if startupID == "" || !t.Has(startupID) {
		return orphans
	}
	startup := t.Get(startupID)
	var remaining []Event
	for _, e := range orphans {
		if IsOrphanedPollingEvent(e) {
			e["_mergedToStartup"] = true
			startup = append(startup, e)
		} else {
			remaining = append(remaining, e)
		}
	}
	t.Set(startupID, startup)
	return remaining
}

// TraceGroup is the distiller-shaped trace used by RehomeOrphanedValueChanges.
type TraceGroup struct {
	TraceID      string
	Events       []Event
	FirstPerfTs  float64
	HasFirstPerf bool
}

// RehomeOrphanedValueChanges re-homes orphaned value:change events to the
// nearest interaction trace by time distance. Used by the distiller where
// traces are { traceId, events[], firstPerfTs }.
func RehomeOrphanedValueChanges(traces []*TraceGroup, sortKey SortKeyFn) {
	if sortKey == nil {
		sortKey = DefaultSortKey
	}
	type orphan struct {
		event Event
		from  *TraceGroup
	}
	var orphans []orphan
	for _, tg := range traces {
		hasInteraction := false
		for _, e := range tg.Events {
			if eventString(e, "kind") == "interaction" {
				hasInteraction = true
				break
			}
		}
		if hasInteraction {
			continue
		}
		for _, e := range tg.Events {
			if eventString(e, "kind") == "value:change" {
				orphans = append(orphans, orphan{event: e, from: tg})
			}
		}
	}
	if len(orphans) == 0 {
		return
	}

	for _, o := range orphans {
		vcTs := sortKey(o.event)
		var best *TraceGroup
		bestDist := math.Inf(1)
		for _, tg := range traces {
			hasInteraction := false
			for _, e := range tg.Events {
				if eventString(e, "kind") == "interaction" {
					hasInteraction = true
					break
				}
			}
			if !hasInteraction {
				continue
			}
			firstTs := tg.FirstPerfTs
			if !tg.HasFirstPerf {
				firstTs = math.Inf(1)
				for _, e := range tg.Events {
					ts := sortKey(e)
					if ts == 0 {
						ts = math.Inf(1)
					}
					if ts < firstTs {
						firstTs = ts
					}
				}
			}
			dist := math.Abs(firstTs - vcTs)
			if dist < bestDist {
				best = tg
				bestDist = dist
			}
		}
		if best != nil {
			best.Events = append(best.Events, o.event)
			if o.from != best {
				for i, e := range o.from.Events {
					if sameEvent(e, o.event) {
						o.from.Events = append(o.from.Events[:i], o.from.Events[i+1:]...)
						break
					}
				}
			}
		}
	}
}

// sameEvent returns true if a and b reference the same underlying map.
// JS code compares events by reference identity (`events.indexOf(vc)`); Go
// approximates that for map types via the data pointer reflect exposes.
func sameEvent(a, b Event) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

// ---------------------------------------------------------------------------
// Filtering
// ---------------------------------------------------------------------------

// FilterPollingFromInteractions filters polling events out of interaction
// traces.
func FilterPollingFromInteractions(t *TracesMap) {
	for _, traceID := range t.Keys() {
		if !strings.HasPrefix(traceID, "i-") {
			continue
		}
		entries := t.Get(traceID)
		filtered := entries[:0]
		for _, e := range entries {
			if !IsPollingEvent(e) {
				filtered = append(filtered, e)
			}
		}
		// Allocate fresh to avoid surprising aliasing with the input slice.
		out := make([]Event, len(filtered))
		copy(out, filtered)
		t.Set(traceID, out)
	}
}

// ---------------------------------------------------------------------------
// Coalescing / deduplication
// ---------------------------------------------------------------------------

// CoalesceValueChanges keeps only the last value:change per component.
// Returns events in insertion order of first appearance per component.
func CoalesceValueChanges(events []Event) []Event {
	byComponent := map[string]Event{}
	order := []string{}
	for _, e := range events {
		if eventString(e, "kind") != "value:change" {
			continue
		}
		comp := eventString(e, "component")
		if _, ok := byComponent[comp]; !ok {
			order = append(order, comp)
		}
		byComponent[comp] = e
	}
	out := make([]Event, 0, len(order))
	for _, comp := range order {
		out = append(out, byComponent[comp])
	}
	return out
}

// DedupResult is the output of DedupByFingerprint.
type DedupResult struct {
	Unique       []DedupEntry
	DedupedCount int
}

// DedupEntry pairs a representative event with its occurrence count.
type DedupEntry struct {
	Event Event
	Count int
}

// DedupByFingerprint groups events by keyFn. keyFn returns "" to skip an
// event. Returns the unique events with counts and a total of duplicate
// occurrences.
func DedupByFingerprint(events []Event, keyFn func(Event) (string, bool)) DedupResult {
	type bucket struct {
		entry *DedupEntry
	}
	seen := map[string]*DedupEntry{}
	order := []string{}
	deduped := 0
	for _, e := range events {
		key, ok := keyFn(e)
		if !ok {
			continue
		}
		if existing, ok := seen[key]; ok {
			existing.Count++
			deduped++
		} else {
			d := &DedupEntry{Event: e, Count: 1}
			seen[key] = d
			order = append(order, key)
		}
	}
	out := make([]DedupEntry, 0, len(order))
	for _, k := range order {
		out = append(out, *seen[k])
	}
	return DedupResult{Unique: out, DedupedCount: deduped}
}

// ---------------------------------------------------------------------------
// Full preprocessing pipeline (viewer-oriented)
// ---------------------------------------------------------------------------

// PreprocessResult is the return value of PreprocessTraces.
type PreprocessResult struct {
	TracesMap      *TracesMap
	Orphans        []Event
	StartupTraceID string
}

// PreprocessTraces runs the full preprocessing pipeline used by the viewer.
// Takes raw entries (excluding standalone interactions). Mutates entries
// (via MatchAPIPairs) and returns the grouped trace map.
func PreprocessTraces(entries []Event, sortKey SortKeyFn) PreprocessResult {
	if sortKey == nil {
		sortKey = DefaultSortKey
	}

	MatchAPIPairs(entries)

	tracesMap, orphans := GroupByTraceID(entries)

	FindOrCreateStartupTraceID(tracesMap)
	orphans = MergeBootstrapOrphans(tracesMap, orphans, sortKey)

	MergePollingTraces(tracesMap)

	orphans = MergeChangeListenerOrphans(tracesMap, orphans)

	currentStartupID := ""
	for _, k := range tracesMap.Keys() {
		if strings.HasPrefix(k, "startup-") {
			currentStartupID = k
			break
		}
	}
	orphans = RehomeByTimeWindow(tracesMap, orphans, RehomeOptions{
		Buffer:         500,
		bufferProvided: true,
		SourceTraceID:  currentStartupID,
	})

	orphans = MergeOrphanedPollingToStartup(tracesMap, orphans)

	FilterPollingFromInteractions(tracesMap)

	return PreprocessResult{
		TracesMap:      tracesMap,
		Orphans:        orphans,
		StartupTraceID: currentStartupID,
	}
}
