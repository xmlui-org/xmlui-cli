package distillcmd

import (
	"math"
	"sort"
	"strings"
)

// ParsedTrace is the structure produced by ParseTrace — one trace group per
// traceId (orphans bucketed under "orphan"). Used by the xs-diff viewer; not
// the same shape as the distiller's output.
type ParsedTrace struct {
	TraceID    string  `json:"traceId"`
	Summary    string  `json:"summary"`
	DurationMs int     `json:"durationMs"`
	Events     []Event `json:"events"`
}

// ParseTrace groups raw events by traceId, computes a duration and a summary
// label, and returns the traces sorted by first event time. Mirrors
// trace-tools/parse-trace.js parseTrace().
func ParseTrace(events []Event) []ParsedTrace {
	groups := map[string][]Event{}
	order := []string{}
	for _, e := range events {
		tid := eventString(e, "traceId")
		if tid == "" {
			tid = "orphan"
		}
		if _, ok := groups[tid]; !ok {
			order = append(order, tid)
		}
		groups[tid] = append(groups[tid], e)
	}

	out := make([]ParsedTrace, 0, len(order))
	for _, tid := range order {
		evs := groups[tid]
		out = append(out, ParsedTrace{
			TraceID:    tid,
			Summary:    parseTraceSummary(tid, evs),
			DurationMs: parseTraceDuration(evs),
			Events:     evs,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		var ai, bj float64
		if len(out[i].Events) > 0 {
			if v, ok := eventNumber(out[i].Events[0], "perfTs"); ok {
				ai = v
			}
		}
		if len(out[j].Events) > 0 {
			if v, ok := eventNumber(out[j].Events[0], "perfTs"); ok {
				bj = v
			}
		}
		return ai < bj
	})
	return out
}

func parseTraceSummary(traceID string, events []Event) string {
	if strings.HasPrefix(traceID, "startup") {
		return "Startup"
	}
	var interaction, handler Event
	for _, e := range events {
		switch eventString(e, "kind") {
		case "interaction":
			if interaction == nil {
				interaction = e
			}
		case "handler:start":
			if handler == nil {
				handler = e
			}
		}
	}
	if handler != nil {
		comp := eventString(handler, "componentLabel")
		if comp == "" {
			comp = eventString(handler, "componentType")
		}
		eventName := eventString(handler, "eventName")
		if comp != "" {
			return strings.TrimSpace(comp + " " + eventName)
		}
		return eventName
	}
	if interaction != nil {
		comp := eventString(interaction, "componentLabel")
		action := eventString(interaction, "interaction")
		if action == "" {
			action = "click"
		}
		return strings.TrimSpace(comp + " " + action)
	}
	if len(events) > 0 {
		k := eventString(events[0], "kind")
		if k != "" {
			return k
		}
	}
	return "unknown"
}

func parseTraceDuration(events []Event) int {
	min := math.Inf(1)
	max := math.Inf(-1)
	count := 0
	for _, e := range events {
		if v, ok := eventNumber(e, "perfTs"); ok {
			count++
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}
	}
	if count < 2 {
		return 0
	}
	return int(math.Round(max - min))
}
