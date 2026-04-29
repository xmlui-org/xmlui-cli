package distillcmd

// Port of trace-tools/distill-trace.js — turns raw xs log events into a
// per-step user-journey summary suitable for narration or replay.

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// DistillOutput is the top-level result of DistillTrace.
type DistillOutput struct {
	Steps []map[string]interface{} `json:"steps"`
}

// DistillTrace produces the distilled JSON for a list of raw xs log events.
// Mirrors distill-trace.js distillTrace().
func DistillTrace(logs []Event) DistillOutput {
	// Modifier-key timeline (Control/Meta/Shift/Alt) built from all
	// interaction keydown/keyup events. Used to infer modifiers on clicks
	// whose own trace doesn't include them.
	modifierKeys := map[string]bool{"Control": true, "Meta": true, "Shift": true, "Alt": true}
	type modifierEntry struct {
		perfTs float64
		key    string
		active bool
	}
	var timeline []modifierEntry
	for _, log := range logs {
		if eventString(log, "kind") != "interaction" {
			continue
		}
		action := eventString(log, "interaction")
		if action == "" {
			action = eventString(log, "eventName")
		}
		if action != "keydown" && action != "keyup" {
			continue
		}
		detail, _ := log["detail"].(map[string]interface{})
		if detail == nil {
			continue
		}
		key, _ := detail["key"].(string)
		if !modifierKeys[key] {
			continue
		}
		ts, _ := eventNumber(log, "perfTs")
		timeline = append(timeline, modifierEntry{perfTs: ts, key: key, active: action == "keydown"})
	}
	sort.SliceStable(timeline, func(i, j int) bool {
		return timeline[i].perfTs < timeline[j].perfTs
	})

	const maxModifierHoldMs = 500.0
	getActiveModifiers := func(perfTs float64) []string {
		active := map[string]bool{}
		lastKeydown := map[string]float64{}
		for _, entry := range timeline {
			if entry.perfTs > perfTs {
				break
			}
			if entry.active {
				active[entry.key] = true
				lastKeydown[entry.key] = entry.perfTs
			} else {
				delete(active, entry.key)
				delete(lastKeydown, entry.key)
			}
		}
		// Drop modifiers whose keydown is too far in the past (missing keyup).
		for k := range active {
			if perfTs-lastKeydown[k] > maxModifierHoldMs {
				delete(active, k)
			}
		}
		out := []string{}
		for k := range active {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}

	// Submenu open events globally (often have no traceId).
	type submenuEntry struct {
		ts    float64
		label string
	}
	var submenuOpens []submenuEntry
	for _, e := range logs {
		if eventString(e, "kind") != "submenu:open" {
			continue
		}
		ts, ok := eventNumber(e, "perfTs")
		if !ok {
			ts, _ = eventNumber(e, "ts")
		}
		label := eventString(e, "ariaName")
		if label == "" {
			label = eventString(e, "componentLabel")
		}
		submenuOpens = append(submenuOpens, submenuEntry{ts: ts, label: label})
	}

	// Defense-in-depth: detect post-interaction events with startup traceId
	// and strip them from the startup group.
	firstInteractionTs := math.Inf(1)
	for _, e := range logs {
		if eventString(e, "kind") != "interaction" {
			continue
		}
		ts, ok := eventNumber(e, "perfTs")
		if !ok {
			ts, ok = eventNumber(e, "ts")
		}
		if !ok {
			continue
		}
		if ts < firstInteractionTs {
			firstInteractionTs = ts
		}
	}

	// Group logs by traceId (with the startup-after-interaction reattribution).
	traceOrder := []string{}
	traceMap := map[string]*TraceGroup{}
	for _, log := range logs {
		traceID := eventString(log, "traceId")
		if traceID == "" {
			traceID = "unknown"
		}
		if strings.HasPrefix(traceID, "startup-") && !math.IsInf(firstInteractionTs, 1) {
			ts, ok := eventNumber(log, "perfTs")
			if !ok {
				ts, _ = eventNumber(log, "ts")
			}
			if ts > firstInteractionTs {
				traceID = "unknown"
			}
		}
		if _, ok := traceMap[traceID]; !ok {
			ts, _ := eventNumber(log, "perfTs")
			traceMap[traceID] = &TraceGroup{TraceID: traceID, FirstPerfTs: ts, HasFirstPerf: true}
			traceOrder = append(traceOrder, traceID)
		}
		tg := traceMap[traceID]
		tg.Events = append(tg.Events, log)
	}

	// Re-home orphaned value:change events (shared with normalize).
	traceArr := make([]*TraceGroup, 0, len(traceOrder))
	for _, tid := range traceOrder {
		traceArr = append(traceArr, traceMap[tid])
	}
	RehomeOrphanedValueChanges(traceArr, func(e Event) float64 {
		if v, ok := eventNumber(e, "perfTs"); ok {
			return v
		}
		v, _ := eventNumber(e, "ts")
		return v
	})

	// Sort by first event time.
	sort.SliceStable(traceArr, func(i, j int) bool {
		return traceArr[i].FirstPerfTs < traceArr[j].FirstPerfTs
	})

	// Collect ariaName from non-interaction trace groups for later propagation.
	type ariaEntry struct {
		ts       float64
		ariaName string
	}
	var ariaNameByTs []ariaEntry
	for _, trace := range traceArr {
		hasInteraction := false
		for _, e := range trace.Events {
			if eventString(e, "kind") == "interaction" {
				hasInteraction = true
				break
			}
		}
		if hasInteraction {
			continue
		}
		for _, e := range trace.Events {
			ariaName := eventString(e, "ariaName")
			kind := eventString(e, "kind")
			if ariaName != "" && (kind == "value:change" || kind == "focus:change" || strings.HasPrefix(kind, "native:")) {
				ts, ok := eventNumber(e, "perfTs")
				if !ok {
					ts, _ = eventNumber(e, "ts")
				}
				ariaNameByTs = append(ariaNameByTs, ariaEntry{ts: ts, ariaName: ariaName})
				break // one per trace group is enough
			}
		}
	}

	// Per-trace step extraction.
	steps := []map[string]interface{}{}
	for _, trace := range traceArr {
		step := extractStepFromJSONLogs(trace)
		if step == nil {
			continue
		}
		step["_firstPerfTs"] = trace.FirstPerfTs

		// Click/dblclick modifier inference from the global timeline.
		action, _ := step["action"].(string)
		target, _ := step["target"].(map[string]interface{})
		if (action == "click" || action == "dblclick") && target != nil &&
			!boolField(target, "ctrlKey") && !boolField(target, "metaKey") &&
			!boolField(target, "shiftKey") && !boolField(target, "altKey") {
			mods := getActiveModifiers(trace.FirstPerfTs)
			if len(mods) > 0 {
				for _, m := range mods {
					switch m {
					case "Control":
						target["ctrlKey"] = true
					case "Meta":
						target["metaKey"] = true
					case "Shift":
						target["shiftKey"] = true
					case "Alt":
						target["altKey"] = true
					}
				}
			}
		}
		steps = append(steps, step)
	}

	// Diff consecutive DataSource snapshots on mutating steps.
	prevSnapshots := map[string][]string{}
	for _, step := range steps {
		snaps, _ := step["_dataSourceSnapshots"].(map[string][]string)
		if snaps == nil {
			continue
		}
		hasMutation := false
		if awaitMap, ok := step["await"].(map[string]interface{}); ok {
			if apis, ok := awaitMap["api"].([]map[string]interface{}); ok {
				for _, a := range apis {
					m, _ := a["method"].(string)
					switch m {
					case "POST", "PUT", "DELETE", "PATCH":
						hasMutation = true
					}
				}
			}
		}
		for dsPath, labels := range snaps {
			if prev, ok := prevSnapshots[dsPath]; ok && hasMutation {
				prevSet := setOf(prev)
				currSet := setOf(labels)
				var added, removed []string
				for _, l := range labels {
					if !prevSet[l] {
						added = append(added, l)
					}
				}
				for _, l := range prev {
					if !currSet[l] {
						removed = append(removed, l)
					}
				}
				if len(added) > 0 || len(removed) > 0 {
					changes, _ := step["dataSourceChanges"].([]map[string]interface{})
					change := map[string]interface{}{"source": dsPath}
					if added == nil {
						added = []string{}
					}
					if removed == nil {
						removed = []string{}
					}
					change["added"] = added
					change["removed"] = removed
					changes = append(changes, change)
					step["dataSourceChanges"] = changes
				}
			}
			prevSnapshots[dsPath] = labels
		}
		delete(step, "_dataSourceSnapshots")
	}

	// Propagate submenu parent to next menuitem click.
	if len(submenuOpens) > 0 {
		subIdx := 0
		for _, step := range steps {
			stepTs, _ := step["_firstPerfTs"].(float64)
			for subIdx < len(submenuOpens)-1 && submenuOpens[subIdx+1].ts < stepTs {
				subIdx++
			}
			target, _ := step["target"].(map[string]interface{})
			if target == nil {
				continue
			}
			if eventStringFromMap(target, "ariaRole") == "menuitem" &&
				subIdx < len(submenuOpens) && submenuOpens[subIdx].ts < stepTs {
				step["submenuParent"] = submenuOpens[subIdx].label
				subIdx++
			}
		}
	}

	// Propagate ariaName from valueChanges to target if missing.
	for _, step := range steps {
		target, _ := step["target"].(map[string]interface{})
		if target == nil {
			continue
		}
		if eventStringFromMap(target, "ariaName") != "" {
			continue
		}
		valueChanges, _ := step["valueChanges"].([]map[string]interface{})
		for _, vc := range valueChanges {
			if name := eventStringFromMap(vc, "ariaName"); name != "" {
				target["ariaName"] = name
				break
			}
		}
	}

	// Propagate ariaName from non-interaction trace groups.
	if len(ariaNameByTs) > 0 {
		ariaIdx := 0
		for i, step := range steps {
			stepTs, _ := step["_firstPerfTs"].(float64)
			for ariaIdx < len(ariaNameByTs) && ariaNameByTs[ariaIdx].ts <= stepTs {
				ariaIdx++
			}
			nextStepTs := math.Inf(1)
			if i+1 < len(steps) {
				if v, ok := steps[i+1]["_firstPerfTs"].(float64); ok {
					nextStepTs = v
				}
			}
			target, _ := step["target"].(map[string]interface{})
			if target == nil || eventStringFromMap(target, "ariaName") != "" {
				continue
			}
			for j := ariaIdx; j < len(ariaNameByTs) && ariaNameByTs[j].ts < nextStepTs; j++ {
				target["ariaName"] = ariaNameByTs[j].ariaName
				break
			}
		}
	}

	// Collapse consecutive textbox keydowns into fill steps.
	type rawVC struct {
		componentLabel string
		ariaName       string
		value          string
		hasValue       bool
		perfTs         float64
	}
	var rawValueChanges []rawVC
	for _, e := range logs {
		if eventString(e, "kind") != "value:change" {
			continue
		}
		ts, _ := eventNumber(e, "perfTs")
		v := rawVC{
			componentLabel: eventString(e, "componentLabel"),
			ariaName:       eventString(e, "ariaName"),
			perfTs:         ts,
		}
		if dl, ok := e["displayLabel"]; ok && dl != nil {
			v.value = fmt.Sprint(dl)
			v.hasValue = true
		}
		rawValueChanges = append(rawValueChanges, v)
	}

	collapsed := []map[string]interface{}{}
	i := 0
	for i < len(steps) {
		step := steps[i]
		action, _ := step["action"].(string)
		target, _ := step["target"].(map[string]interface{})
		ariaRole := eventStringFromMap(target, "ariaRole")
		ariaName := eventStringFromMap(target, "ariaName")
		componentID := eventStringFromMap(target, "componentId")

		if action == "keydown" && ariaRole == "textbox" && (ariaName != "" || componentID != "") {
			startTs, _ := step["_firstPerfTs"].(float64)
			hasDynamicLabel := ariaName != "" && strings.Contains(ariaName, ":")
			matchByComponentID := ariaName == "" || hasDynamicLabel

			j := i
			for j < len(steps) {
				st := steps[j]
				if eventStringFromMap(st, "action") != "keydown" {
					break
				}
				tg, _ := st["target"].(map[string]interface{})
				if eventStringFromMap(tg, "ariaRole") != "textbox" {
					break
				}
				if matchByComponentID {
					if eventStringFromMap(tg, "componentId") != componentID {
						break
					}
				} else {
					if eventStringFromMap(tg, "ariaName") != ariaName {
						break
					}
				}
				j++
			}

			endStep := steps[j-1]
			endTs, _ := endStep["_firstPerfTs"].(float64)
			if endTs == 0 {
				endTs = startTs
			}

			// Match raw value:change events for this textbox.
			finalValue := ""
			for _, vc := range rawValueChanges {
				idMatch := false
				if componentID != "" && vc.componentLabel == componentID {
					idMatch = true
				}
				if vc.ariaName == ariaName && ariaName != "" {
					idMatch = true
				}
				if ariaName != "" && vc.ariaName != "" {
					ap := strings.SplitN(ariaName, ":", 2)[0]
					vp := strings.SplitN(vc.ariaName, ":", 2)[0]
					if ap == vp {
						idMatch = true
					}
				}
				if !idMatch {
					continue
				}
				if vc.perfTs >= startTs && vc.perfTs <= endTs+500 {
					if vc.hasValue {
						finalValue = vc.value
					}
				}
			}

			// Build the fill step.
			fill := map[string]interface{}{
				"action":        "fill",
				"target":        copyMap(target),
				"fillValue":     finalValue,
				"_firstPerfTs":  startTs,
			}
			collapsed = append(collapsed, fill)
			i = j
		} else {
			collapsed = append(collapsed, step)
			i++
		}
	}
	steps = collapsed

	// Strip internal metadata.
	for _, step := range steps {
		delete(step, "_submenuOpens")
		delete(step, "_firstPerfTs")
	}

	// Dedupe click + click + dblclick on same testId → keep dblclick.
	deduped := []map[string]interface{}{}
	i = 0
	for i < len(steps) {
		step := steps[i]
		var next, nextNext map[string]interface{}
		if i+1 < len(steps) {
			next = steps[i+1]
		}
		if i+2 < len(steps) {
			nextNext = steps[i+2]
		}
		if next != nil && nextNext != nil &&
			eventStringFromMap(step, "action") == "click" &&
			eventStringFromMap(next, "action") == "click" &&
			eventStringFromMap(nextNext, "action") == "dblclick" {
			st := targetField(step, "testId")
			nt := targetField(next, "testId")
			nnt := targetField(nextNext, "testId")
			if st != "" && st == nt && st == nnt {
				deduped = append(deduped, nextNext)
				i += 3
				continue
			}
		}
		deduped = append(deduped, step)
		i++
	}

	// Coalesce consecutive keydown valueChanges runs.
	coalesced := []map[string]interface{}{}
	i = 0
	for i < len(deduped) {
		step := deduped[i]
		valueChanges, _ := step["valueChanges"].([]map[string]interface{})
		action := eventStringFromMap(step, "action")
		if len(valueChanges) > 0 && action == "keydown" {
			j := i + 1
			ariaRole := targetField(step, "ariaRole")
			ariaName := targetField(step, "ariaName")
			for j < len(deduped) {
				st := deduped[j]
				if eventStringFromMap(st, "action") != action {
					break
				}
				if targetField(st, "ariaRole") != ariaRole || targetField(st, "ariaName") != ariaName {
					break
				}
				stVCs, _ := st["valueChanges"].([]map[string]interface{})
				if len(stVCs) == 0 {
					break
				}
				j++
			}
			lastStep := deduped[j-1]
			count := j - i
			if count > 1 && eventStringFromMap(lastStep, "action") == "keydown" {
				lastStep["keyCount"] = count
				if targetField(lastStep, "key") == "" {
					for k := i; k < j; k++ {
						if key := targetField(deduped[k], "key"); key != "" {
							tg, _ := lastStep["target"].(map[string]interface{})
							if tg == nil {
								tg = map[string]interface{}{}
								lastStep["target"] = tg
							}
							tg["key"] = key
							break
						}
					}
				}
			}
			coalesced = append(coalesced, lastStep)
			i = j
		} else {
			coalesced = append(coalesced, step)
			i++
		}
	}

	if coalesced == nil {
		coalesced = []map[string]interface{}{}
	}
	return DistillOutput{Steps: coalesced}
}

// summarizeResult produces the apiResult shape (snapshot or rowcount) for an
// API response. Returns nil to skip.
func summarizeResult(result interface{}) map[string]interface{} {
	if result == nil {
		return nil
	}
	dateLike := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)
	var compactValue func(v interface{}, depth int) interface{}
	compactValue = func(v interface{}, depth int) interface{} {
		switch x := v.(type) {
		case string:
			if dateLike.MatchString(x) {
				return "__DATE__"
			}
			return x
		case []interface{}:
			if len(x) == 0 {
				return []interface{}{}
			}
			if depth <= 0 {
				return map[string]interface{}{
					"type":  "array",
					"count": len(x),
				}
			}
			allScalars := true
			for _, item := range x {
				switch item.(type) {
				case map[string]interface{}, []interface{}:
					allScalars = false
				}
				if !allScalars {
					break
				}
			}
			if allScalars && len(x) <= 5 {
				out := make([]interface{}, 0, len(x))
				for _, item := range x {
					out = append(out, compactValue(item, depth-1))
				}
				return out
			}
			return map[string]interface{}{
				"type":   "array",
				"count":  len(x),
				"sample": compactValue(x[0], depth-1),
			}
		case map[string]interface{}:
			if depth <= 0 {
				return map[string]interface{}{
					"type": "object",
					"keys": sortedKeys(x),
				}
			}
			out := map[string]interface{}{}
			for _, key := range sortedKeys(x) {
				out[key] = compactValue(x[key], depth-1)
			}
			return out
		default:
			return x
		}
	}

	if arr, ok := result.([]interface{}); ok {
		if len(arr) == 0 {
			return nil
		}
		if len(arr) == 1 {
			obj, ok := arr[0].(map[string]interface{})
			if !ok {
				return nil
			}
			out := map[string]interface{}{
				"type":   "snapshot",
				"keys":   sortedKeys(obj),
				"values": compactValue(obj, 3),
			}
			if summary := weatherSnapshotSummary(obj); summary != nil {
				out["summary"] = summary
			}
			return out
		}
		first, _ := arr[0].(map[string]interface{})
		return map[string]interface{}{
			"type":  "rowcount",
			"count": len(arr),
			"keys":  sortedKeys(first),
		}
	}

	if obj, ok := result.(map[string]interface{}); ok {
		out := map[string]interface{}{
			"type":   "snapshot",
			"keys":   sortedKeys(obj),
			"values": compactValue(obj, 3),
		}
		if summary := weatherSnapshotSummary(obj); summary != nil {
			out["summary"] = summary
		}
		return out
	}

	return nil
}

func weatherSnapshotSummary(obj map[string]interface{}) map[string]interface{} {
	current := firstObjectInArrayField(obj, "current_condition")
	area := firstObjectInArrayField(obj, "nearest_area")
	if current == nil || area == nil {
		return nil
	}
	out := map[string]interface{}{
		"type": "weather",
	}
	if location := firstValueField(area, "areaName"); location != "" {
		out["location"] = location
	}
	if region := firstValueField(area, "region"); region != "" {
		out["region"] = region
	}
	if country := firstValueField(area, "country"); country != "" {
		out["country"] = country
	}
	if tempC, _ := current["temp_C"].(string); tempC != "" {
		out["tempC"] = tempC
	}
	if tempF, _ := current["temp_F"].(string); tempF != "" {
		out["tempF"] = tempF
	}
	if desc := firstValueField(current, "weatherDesc"); desc != "" {
		out["condition"] = strings.TrimSpace(desc)
	}
	if days, ok := obj["weather"].([]interface{}); ok && len(days) > 0 {
		out["forecastDays"] = len(days)
	}
	return out
}

func firstObjectInArrayField(obj map[string]interface{}, key string) map[string]interface{} {
	arr, _ := obj[key].([]interface{})
	if len(arr) == 0 {
		return nil
	}
	first, _ := arr[0].(map[string]interface{})
	return first
}

func firstValueField(obj map[string]interface{}, key string) string {
	arr, _ := obj[key].([]interface{})
	if len(arr) == 0 {
		return ""
	}
	first, _ := arr[0].(map[string]interface{})
	value, _ := first["value"].(string)
	return value
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var (
	httpVerbRegex      = regexp.MustCompile(`^(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)$`)
	queryParamTernary  = regexp.MustCompile(`\{\$queryParams\.(\w+)\s*==\s*'([^']+)'\s*\?\s*'(\w+)'\s*:\s*'(\w+)'\s*\}`)
	httpVerbAnywhere   = regexp.MustCompile(`(?i)\b(get|post|put|delete|patch|head|options)\b`)
)

// resolveMethod resolves an HTTP method that may be an unresolved XMLUI
// expression. Mirrors distill-trace.js resolveMethod().
func resolveMethod(method, urlStr string) string {
	if method == "" {
		return method
	}
	clean := strings.ToUpper(strings.TrimSpace(method))
	if httpVerbRegex.MatchString(clean) {
		return clean
	}

	if m := queryParamTernary.FindStringSubmatch(method); m != nil {
		paramName, paramValue, trueMethod, falseMethod := m[1], m[2], m[3], m[4]
		if urlStr != "" {
			if u, err := url.Parse(urlStr); err == nil {
				if u.Scheme == "" {
					if u2, err2 := url.Parse("http://localhost" + ensureLeadingSlash(urlStr)); err2 == nil {
						u = u2
					}
				}
				if u.Query().Has(paramName) {
					val := u.Query().Get(paramName)
					if val == paramValue {
						return strings.ToUpper(trueMethod)
					}
					return strings.ToUpper(falseMethod)
				}
			}
		}
		if paramName == "new" && paramValue == "true" && urlStr != "" {
			pathOnly := urlStr
			if i := strings.IndexByte(pathOnly, '?'); i >= 0 {
				pathOnly = pathOnly[:i]
			}
			parts := []string{}
			for _, p := range strings.Split(pathOnly, "/") {
				if p != "" {
					parts = append(parts, p)
				}
			}
			if len(parts) > 2 {
				return strings.ToUpper(falseMethod)
			}
			return strings.ToUpper(trueMethod)
		}
		return strings.ToUpper(trueMethod)
	}

	if m := httpVerbAnywhere.FindStringSubmatch(method); m != nil {
		return strings.ToUpper(m[1])
	}

	return method
}

func ensureLeadingSlash(s string) string {
	if !strings.HasPrefix(s, "/") {
		return "/" + s
	}
	return s
}

// itemLabel picks a human-readable label out of an arbitrary DataSource item.
func itemLabel(obj interface{}) string {
	m, ok := obj.(map[string]interface{})
	if !ok {
		return ""
	}
	for _, key := range []string{"name", "title", "label", "displayName", "username"} {
		if v, ok := m[key].(string); ok && v != "" {
			return v
		}
	}
	for _, k := range sortedKeys(m) {
		if v, ok := m[k].(string); ok && v != "" && len(v) < 80 {
			return v
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Small helpers used by the distiller
// ---------------------------------------------------------------------------

func boolField(m map[string]interface{}, key string) bool {
	if m == nil {
		return false
	}
	v, _ := m[key].(bool)
	return v
}

func eventStringFromMap(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

func targetField(step map[string]interface{}, key string) string {
	tg, _ := step["target"].(map[string]interface{})
	return eventStringFromMap(tg, key)
}

func setOf(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, s := range items {
		out[s] = true
	}
	return out
}

func copyMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
