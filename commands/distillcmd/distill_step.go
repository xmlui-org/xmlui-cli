package distillcmd

import (
	"encoding/json"
	"math"
	"regexp"
	"strings"
)

// extractStepFromJSONLogs converts a single trace group into a distilled step
// (or nil if it should be skipped). Mirrors distill-trace.js
// extractStepFromJsonLogs().
func extractStepFromJSONLogs(trace *TraceGroup) map[string]interface{} {
	events := trace.Events

	// Pick the best interaction event — prefer one with non-banner ariaRole.
	var interaction Event
	var firstInteraction Event
	for _, e := range events {
		if eventString(e, "kind") != "interaction" {
			continue
		}
		if firstInteraction == nil {
			firstInteraction = e
		}
		role := eventString(e, "ariaRole")
		if role != "" && role != "banner" && interaction == nil {
			interaction = e
		}
	}
	if interaction == nil {
		interaction = firstInteraction
	}

	// Startup case (no interaction, traceId starts with "startup-").
	if interaction == nil && strings.HasPrefix(trace.TraceID, "startup-") {
		var rawAPIs []map[string]interface{}
		for _, e := range events {
			if eventString(e, "kind") != "api:complete" || eventString(e, "method") == "" {
				continue
			}
			endpoint := eventString(e, "url")
			if endpoint == "" {
				endpoint = eventString(e, "endpoint")
			}
			entry := map[string]interface{}{
				"method":   resolveMethod(eventString(e, "method"), endpoint),
				"endpoint": endpoint,
			}
			if summary := summarizeResult(e["result"]); summary != nil {
				entry["apiResult"] = summary
			}
			rawAPIs = append(rawAPIs, entry)
		}
		seen := map[string]bool{}
		var apiCalls []map[string]interface{}
		for _, api := range rawAPIs {
			endpoint, _ := api["endpoint"].(string)
			base := endpoint
			if i := strings.IndexByte(base, '?'); i >= 0 {
				base = base[:i]
			}
			method, _ := api["method"].(string)
			key := method + " " + base
			if !seen[key] {
				seen[key] = true
				apiCalls = append(apiCalls, api)
			}
		}

		// app:trace events from startup.
		var startupAppTraces []Event
		for _, e := range events {
			if eventString(e, "kind") == "app:trace" && e["data"] != nil {
				startupAppTraces = append(startupAppTraces, e)
			}
		}
		startup := map[string]interface{}{
			"action": "startup",
			"await":  map[string]interface{}{"api": ensureSliceOfMaps(apiCalls)},
		}
		if len(startupAppTraces) > 0 {
			byLabel := map[string][]interface{}{}
			for _, e := range startupAppTraces {
				label := eventString(e, "label")
				if label == "" {
					label = "unknown"
				}
				byLabel[label] = append(byLabel[label], e["data"])
			}
			startup["appTraces"] = byLabel
		}
		return startup
	}

	if interaction == nil {
		// Toast-only trace groups.
		var toasts []map[string]interface{}
		for _, e := range events {
			if eventString(e, "kind") != "toast" {
				continue
			}
			toastType := eventString(e, "toastType")
			if toastType == "" {
				toastType = "default"
			}
			toasts = append(toasts, map[string]interface{}{
				"type":    toastType,
				"message": eventString(e, "message"),
			})
		}
		if len(toasts) > 0 {
			return map[string]interface{}{
				"action": "toast",
				"toasts": toasts,
			}
		}
		return nil
	}

	// Skip Inspector UI.
	if eventString(interaction, "componentLabel") == "XMLUI Inspector" ||
		eventString(interaction, "componentType") == "XSInspector" {
		return nil
	}
	if detail, ok := interaction["detail"].(map[string]interface{}); ok {
		if text, _ := detail["text"].(string); strings.Contains(text, "XMLUI Inspector") {
			return nil
		}
	}

	target := map[string]interface{}{}
	componentType := eventString(interaction, "componentType")
	componentLabel := eventString(interaction, "componentLabel")
	if componentType != "" {
		target["component"] = componentType
	} else if componentLabel != "" {
		target["component"] = componentLabel
	}
	target["label"] = nil

	// componentId for cross-event matching.
	componentID := eventString(interaction, "uid")
	if componentID == "" {
		if detail, ok := interaction["detail"].(map[string]interface{}); ok {
			componentID, _ = detail["componentId"].(string)
		}
	}
	if componentID != "" {
		target["componentId"] = componentID
	}

	// targetTag.
	if detail, ok := interaction["detail"].(map[string]interface{}); ok {
		if tt, _ := detail["targetTag"].(string); tt != "" {
			target["targetTag"] = tt
		}
	}

	// Canvas clicks.
	tagUpper := strings.ToUpper(eventStringFromMap(target, "targetTag"))
	if tagUpper == "CANVAS" {
		for _, e := range events {
			kind := eventString(e, "kind")
			if !strings.HasPrefix(kind, "native:") {
				continue
			}
			ox, hasOX := eventNumber(e, "offsetX")
			oy, hasOY := eventNumber(e, "offsetY")
			if !hasOX {
				continue
			}
			target["canvasX"] = int(math.Round(ox))
			if hasOY {
				target["canvasY"] = int(math.Round(oy))
			}
			if name := eventString(e, "ariaName"); name != "" {
				target["ariaName"] = name
			}
			if dl := eventString(e, "displayLabel"); dl != "" {
				target["label"] = dl
			}
			break
		}
	}

	// selectorPath.
	if detail, ok := interaction["detail"].(map[string]interface{}); ok {
		if sp, _ := detail["selectorPath"].(string); sp != "" {
			target["selectorPath"] = sp
		}
		if v, _ := detail["ctrlKey"].(bool); v {
			target["ctrlKey"] = true
		}
		if v, _ := detail["shiftKey"].(bool); v {
			target["shiftKey"] = true
		}
		if v, _ := detail["metaKey"].(bool); v {
			target["metaKey"] = true
		}
		if v, _ := detail["altKey"].(bool); v {
			target["altKey"] = true
		}
		if r, _ := detail["ariaRole"].(string); r != "" {
			target["ariaRole"] = r
		}
		if n, _ := detail["ariaName"].(string); n != "" {
			target["ariaName"] = n
		}
	}
	if eventStringFromMap(target, "ariaRole") == "" {
		if r := eventString(interaction, "ariaRole"); r != "" {
			target["ariaRole"] = r
		}
	}
	if eventStringFromMap(target, "ariaName") == "" {
		if n := eventString(interaction, "ariaName"); n != "" {
			target["ariaName"] = n
		}
	}
	if sal := eventString(interaction, "selectAriaLabel"); sal != "" {
		target["selectAriaLabel"] = sal
	}

	// Behavioral fallback for ariaName.
	if eventStringFromMap(target, "ariaName") == "" {
		for _, e := range events {
			kind := eventString(e, "kind")
			ariaName := eventString(e, "ariaName")
			if ariaName == "" {
				continue
			}
			if kind == "value:change" || kind == "focus:change" || strings.HasPrefix(kind, "native:") {
				target["ariaName"] = ariaName
				break
			}
		}
	}

	// testId fallback.
	if uid := eventString(interaction, "uid"); uid != "" {
		target["testId"] = uid
	}

	// Label from interaction detail.text (only if short and no ariaName).
	if eventStringFromMap(target, "ariaName") == "" {
		if detail, ok := interaction["detail"].(map[string]interface{}); ok {
			if text, _ := detail["text"].(string); text != "" && len(text) < 50 {
				target["label"] = text
			}
		}
	}

	// Handler args: displayName + form data on submit.
	for _, e := range events {
		if eventString(e, "kind") != "handler:start" {
			continue
		}
		var args interface{}
		if ea, ok := e["eventArgs"].([]interface{}); ok && len(ea) > 0 {
			args = ea[0]
		} else if a, ok := e["args"].([]interface{}); ok && len(a) > 0 {
			args = a[0]
		} else if a, ok := e["args"]; ok {
			args = a
		} else {
			continue
		}
		if argMap, ok := args.(map[string]interface{}); ok {
			if dn, _ := argMap["displayName"].(string); dn != "" {
				target["label"] = dn
				target["selector"] = map[string]interface{}{
					"role": "treeitem",
					"name": dn,
				}
			}
		}
		if eventString(e, "eventName") == "submit" && args != nil {
			target["formData"] = args
		}
		break
	}

	// formData fallback from mutating API body.
	if _, ok := target["formData"]; !ok {
		for _, e := range events {
			if eventString(e, "kind") != "api:start" {
				continue
			}
			body, ok := e["body"].(map[string]interface{})
			if !ok || body == nil {
				continue
			}
			method := strings.ToUpper(resolveMethod(eventString(e, "method"), eventString(e, "url")))
			if method == "POST" || method == "PUT" || method == "PATCH" {
				target["formData"] = body
				break
			}
		}
	}

	// Selection from state:changes diffJson.
	for _, e := range events {
		if eventString(e, "kind") != "state:changes" {
			continue
		}
		diffs := eventArray(e, "diffJson")
		for _, d := range diffs {
			dm, ok := d.(map[string]interface{})
			if !ok {
				continue
			}
			path, _ := dm["path"].(string)
			if !strings.Contains(path, "selectedIds") {
				continue
			}
			after := dm["after"]
			if after == nil {
				continue
			}
			var selected string
			if arr, ok := after.([]interface{}); ok && len(arr) > 0 {
				selected, _ = arr[0].(string)
			} else {
				selected, _ = after.(string)
			}
			if selected == "" {
				continue
			}
			parts := strings.Split(selected, "/")
			name := parts[len(parts)-1]
			if eventStringFromMap(target, "label") == "" {
				target["label"] = name
			}
			target["selectedPath"] = selected
		}
	}

	// keydown: preserve key.
	intAction := eventString(interaction, "interaction")
	if intAction == "" {
		intAction = eventString(interaction, "eventName")
	}
	if intAction == "keydown" {
		if detail, ok := interaction["detail"].(map[string]interface{}); ok {
			if key, _ := detail["key"].(string); key != "" {
				target["key"] = key
			}
		}
	}

	// Fallback label from componentLabel (filtered).
	if eventStringFromMap(target, "label") == "" && componentLabel != "" {
		if !isGenericLabel(componentLabel) && !isHTMLTag(componentLabel) {
			target["label"] = componentLabel
		}
	}

	// awaitConditions.
	awaitMap := map[string]interface{}{}
	type apiEntry struct {
		method    string
		endpoint  string
		status    interface{}
		apiResult map[string]interface{}
	}
	apiByKey := map[string]*apiEntry{}
	apiOrder := []string{}
	for _, e := range events {
		kind := eventString(e, "kind")
		if (kind != "api:complete" && kind != "api:start") || eventString(e, "method") == "" {
			continue
		}
		endpoint := eventString(e, "url")
		if endpoint == "" {
			endpoint = eventString(e, "endpoint")
		}
		method := resolveMethod(eventString(e, "method"), endpoint)
		base := endpoint
		if i := strings.IndexByte(base, '?'); i >= 0 {
			base = base[:i]
		}
		key := method + " " + base
		entry, exists := apiByKey[key]
		newEntry := &apiEntry{method: method, endpoint: endpoint}
		if status, ok := e["status"]; ok {
			newEntry.status = status
		}
		if kind == "api:complete" {
			if summary := summarizeResult(e["result"]); summary != nil {
				newEntry.apiResult = summary
			}
		}
		if !exists {
			apiByKey[key] = newEntry
			apiOrder = append(apiOrder, key)
		} else if newEntry.apiResult != nil && entry.apiResult == nil {
			apiByKey[key] = newEntry
		}
	}
	if len(apiOrder) > 0 {
		var apiCalls []map[string]interface{}
		for _, key := range apiOrder {
			a := apiByKey[key]
			m := map[string]interface{}{"method": a.method, "endpoint": a.endpoint}
			if a.status != nil {
				m["status"] = a.status
			}
			if a.apiResult != nil {
				m["apiResult"] = a.apiResult
			}
			apiCalls = append(apiCalls, m)
		}
		awaitMap["api"] = apiCalls
	}

	for _, e := range events {
		if eventString(e, "kind") == "navigate" {
			awaitMap["navigate"] = map[string]interface{}{
				"from": eventString(e, "from"),
				"to":   eventString(e, "to"),
			}
			break
		}
	}

	step := map[string]interface{}{
		"action": intAction,
		"target": target,
	}
	if len(awaitMap) > 0 {
		step["await"] = awaitMap
	}

	// Modals.
	if modals := extractModals(events); len(modals) > 0 {
		step["modals"] = modals
	}

	// Toasts.
	var toasts []map[string]interface{}
	for _, e := range events {
		if eventString(e, "kind") != "toast" {
			continue
		}
		t := eventString(e, "toastType")
		if t == "" {
			t = "default"
		}
		toasts = append(toasts, map[string]interface{}{
			"type":    t,
			"message": eventString(e, "message"),
		})
	}
	if len(toasts) > 0 {
		step["toasts"] = toasts
	}

	// valueChanges (last per component).
	type vcEntry struct {
		component      string
		value          interface{}
		hasValue       bool
		ariaName       string
		componentLabel string
		files          interface{}
	}
	vcByComponent := map[string]*vcEntry{}
	vcOrder := []string{}
	for _, e := range events {
		if eventString(e, "kind") != "value:change" {
			continue
		}
		comp := eventString(e, "component")
		entry := &vcEntry{component: comp}
		if dl, ok := e["displayLabel"]; ok && dl != nil {
			entry.value = jsonString(dl)
			entry.hasValue = true
		}
		if name := eventString(e, "ariaName"); name != "" {
			entry.ariaName = name
		}
		if cl := eventString(e, "componentLabel"); cl != "" {
			entry.componentLabel = cl
		}
		if f, ok := e["files"]; ok {
			entry.files = f
		}
		if _, exists := vcByComponent[comp]; !exists {
			vcOrder = append(vcOrder, comp)
		}
		vcByComponent[comp] = entry
	}
	if len(vcOrder) > 0 {
		var vcs []map[string]interface{}
		for _, comp := range vcOrder {
			v := vcByComponent[comp]
			m := map[string]interface{}{"component": v.component}
			if v.hasValue {
				m["value"] = v.value
			}
			if v.ariaName != "" {
				m["ariaName"] = v.ariaName
			}
			if v.componentLabel != "" {
				m["componentLabel"] = v.componentLabel
			}
			if v.files != nil {
				m["files"] = v.files
			}
			vcs = append(vcs, m)
		}
		step["valueChanges"] = vcs
	}

	// FileInput synthesis.
	hasFiles := false
	if vcs, ok := step["valueChanges"].([]map[string]interface{}); ok {
		for _, vc := range vcs {
			if _, ok := vc["files"]; ok {
				hasFiles = true
				break
			}
		}
	}
	if !hasFiles {
		fileInputFiles := extractFileInputFiles(events)
		if len(fileInputFiles) > 0 {
			vcs, _ := step["valueChanges"].([]map[string]interface{})
			vcs = append(vcs, map[string]interface{}{
				"component": "FileInput",
				"files":     fileInputFiles,
			})
			step["valueChanges"] = vcs
		}
	}

	// app:trace events.
	appTraces := map[string][]interface{}{}
	for _, e := range events {
		if eventString(e, "kind") != "app:trace" {
			continue
		}
		if e["data"] == nil {
			continue
		}
		label := eventString(e, "label")
		if label == "" {
			label = "unknown"
		}
		appTraces[label] = append(appTraces[label], e["data"])
	}
	if len(appTraces) > 0 {
		step["appTraces"] = appTraces
	}

	// submenu opens (intermediate).
	var submenuOpens []string
	for _, e := range events {
		if eventString(e, "kind") != "submenu:open" {
			continue
		}
		name := eventString(e, "ariaName")
		if name == "" {
			name = eventString(e, "componentLabel")
		}
		submenuOpens = append(submenuOpens, name)
	}
	if len(submenuOpens) > 0 {
		step["_submenuOpens"] = submenuOpens
	}

	// DataSource snapshots (intermediate).
	dsSnapshots := map[string][]string{}
	for _, e := range events {
		if eventString(e, "kind") != "state:changes" {
			continue
		}
		for _, d := range eventArray(e, "diffJson") {
			dm, ok := d.(map[string]interface{})
			if !ok {
				continue
			}
			path, _ := dm["path"].(string)
			if !strings.HasPrefix(path, "DataSource:") {
				continue
			}
			arr, ok := dm["after"].([]interface{})
			if !ok {
				continue
			}
			labels := []string{}
			for _, item := range arr {
				if l := itemLabel(item); l != "" {
					labels = append(labels, l)
				}
			}
			dsSnapshots[path] = labels
		}
	}
	if len(dsSnapshots) > 0 {
		step["_dataSourceSnapshots"] = dsSnapshots
	}

	// stateDiffs.
	var stateDiffs []map[string]interface{}
	for _, e := range events {
		if eventString(e, "kind") != "state:changes" {
			continue
		}
		for _, d := range eventArray(e, "diffJson") {
			dm, ok := d.(map[string]interface{})
			if !ok {
				continue
			}
			path, _ := dm["path"].(string)
			if path == "" || strings.HasPrefix(path, "DataSource:") {
				continue
			}
			arr, ok := dm["after"].([]interface{})
			if !ok {
				continue
			}
			beforeArr, _ := dm["before"].([]interface{})
			diff := map[string]interface{}{
				"path":   path,
				"before": len(beforeArr),
				"after":  len(arr),
			}
			prevLabels := []string{}
			for _, item := range beforeArr {
				if l := itemLabel(item); l != "" {
					prevLabels = append(prevLabels, l)
				}
			}
			afterLabels := []string{}
			for _, item := range arr {
				if l := itemLabel(item); l != "" {
					afterLabels = append(afterLabels, l)
				}
			}
			prevSet := setOf(prevLabels)
			afterSet := setOf(afterLabels)
			var added, removed []string
			for _, l := range afterLabels {
				if !prevSet[l] {
					added = append(added, l)
				}
			}
			for _, l := range prevLabels {
				if !afterSet[l] {
					removed = append(removed, l)
				}
			}
			if len(added) > 0 {
				diff["added"] = added
			}
			if len(removed) > 0 {
				diff["removed"] = removed
			}
			stateDiffs = append(stateDiffs, diff)
		}
	}
	if len(stateDiffs) > 0 {
		step["stateDiffs"] = stateDiffs
	}

	// validation:error.
	var validationErrors []map[string]interface{}
	for _, e := range events {
		if eventString(e, "kind") != "validation:error" {
			continue
		}
		form := eventString(e, "componentLabel")
		if form == "" {
			form = "Form"
		}
		errFields, _ := e["errorFields"].([]interface{})
		validationErrors = append(validationErrors, map[string]interface{}{
			"form":        form,
			"errorCount":  len(errFields),
			"errorFields": errFields,
		})
	}
	if len(validationErrors) > 0 {
		step["validationErrors"] = validationErrors
	}

	// data:bind.
	var dataBinds []map[string]interface{}
	for _, e := range events {
		if eventString(e, "kind") != "data:bind" {
			continue
		}
		comp := eventString(e, "componentLabel")
		if comp == "" {
			comp = eventString(e, "component")
		}
		entry := map[string]interface{}{"component": comp}
		if v, ok := e["prevCount"]; ok {
			entry["prevCount"] = v
		}
		if v, ok := e["rowCount"]; ok {
			entry["rowCount"] = v
		}
		dataBinds = append(dataBinds, entry)
	}
	if len(dataBinds) > 0 {
		step["dataBinds"] = dataBinds
	}

	return step
}

// extractModals pulls confirmation-dialog sequences out of a trace group.
func extractModals(events []Event) []map[string]interface{} {
	var modalShows []Event
	for _, e := range events {
		if eventString(e, "kind") == "modal:show" {
			modalShows = append(modalShows, e)
		}
	}
	var modals []map[string]interface{}
	for i, show := range modalShows {
		showTs, _ := eventNumber(show, "perfTs")
		if showTs == 0 {
			showTs, _ = eventNumber(show, "ts")
		}
		nextShowTs := math.Inf(1)
		if i+1 < len(modalShows) {
			next := modalShows[i+1]
			t, ok := eventNumber(next, "perfTs")
			if !ok {
				t, _ = eventNumber(next, "ts")
			}
			nextShowTs = t
		}
		var resolution Event
		for _, e := range events {
			kind := eventString(e, "kind")
			if kind != "modal:confirm" && kind != "modal:cancel" {
				continue
			}
			ts, ok := eventNumber(e, "perfTs")
			if !ok {
				ts, _ = eventNumber(e, "ts")
			}
			if ts > showTs && ts <= nextShowTs {
				resolution = e
				break
			}
		}
		modal := map[string]interface{}{}
		if t, ok := show["title"]; ok {
			modal["title"] = t
		}
		if b, ok := show["buttons"]; ok {
			modal["buttons"] = b
		}
		if resolution != nil && eventString(resolution, "kind") == "modal:confirm" {
			modal["action"] = "confirm"
			if v, ok := resolution["value"]; ok {
				modal["value"] = v
			}
			if l, ok := resolution["buttonLabel"].(string); ok && l != "" {
				modal["buttonLabel"] = l
			}
			if _, has := modal["buttonLabel"]; !has {
				if buttons, ok := modal["buttons"].([]interface{}); ok {
					if val, ok := modal["value"]; ok {
						for _, b := range buttons {
							bm, ok := b.(map[string]interface{})
							if !ok {
								continue
							}
							if jsonEqual(bm["value"], val) {
								if lbl, _ := bm["label"].(string); lbl != "" {
									modal["buttonLabel"] = lbl
								}
								break
							}
						}
					}
				}
			}
		} else if resolution != nil && eventString(resolution, "kind") == "modal:cancel" {
			modal["action"] = "cancel"
		} else {
			modal["action"] = "unknown"
		}
		modals = append(modals, modal)
	}
	return modals
}

// extractFileInputFiles synthesizes file metadata from FileInput handler logs
// when no value:change exists.
func extractFileInputFiles(events []Event) []map[string]interface{} {
	for _, e := range events {
		if eventString(e, "kind") == "handler:start" &&
			eventString(e, "componentType") == "FileInput" &&
			eventString(e, "eventName") == "didChange" {
			ea, _ := e["eventArgs"].([]interface{})
			if len(ea) == 0 {
				continue
			}
			fileList, _ := ea[0].([]interface{})
			files := buildFileList(fileList)
			if len(files) > 0 {
				return files
			}
			continue
		}
		// Legacy 'text' field fallback.
		text := eventString(e, "text")
		if text == "" {
			continue
		}
		var parsed []interface{}
		if err := json.Unmarshal([]byte(text), &parsed); err != nil {
			continue
		}
		if len(parsed) < 2 {
			continue
		}
		if first, _ := parsed[0].(string); first != "handler:start" {
			continue
		}
		info, _ := parsed[1].(map[string]interface{})
		if eventStringFromMap(info, "componentType") != "FileInput" || eventStringFromMap(info, "eventName") != "didChange" {
			continue
		}
		args, _ := info["args"].([]interface{})
		if len(args) == 0 {
			continue
		}
		fileList, _ := args[0].([]interface{})
		files := buildFileList(fileList)
		if len(files) > 0 {
			return files
		}
	}
	return nil
}

func buildFileList(raw []interface{}) []map[string]interface{} {
	var files []map[string]interface{}
	for _, f := range raw {
		fm, ok := f.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := fm["path"].(string)
		if name == "" {
			name, _ = fm["name"].(string)
		}
		if name == "" {
			continue
		}
		name = strings.TrimPrefix(name, "./")
		files = append(files, map[string]interface{}{"name": name})
	}
	return files
}

// genericLabelRegex / htmlTagRegex match the JS heuristics for filtering
// unhelpful component labels.
var (
	genericLabelRegex = regexp.MustCompile(`^[A-Z][a-z]+[A-Z]|^(HStack|VStack|Tree|Stack|Box|Link|Text)$`)
	htmlTagRegex      = regexp.MustCompile(`(?i)^(svg|path|input|textarea|div|span|button|a|img|label|select|option|ul|li|ol|tr|td|th|table|form|section|header|footer|nav|main|aside|article)$`)
)

func isGenericLabel(s string) bool { return genericLabelRegex.MatchString(s) }
func isHTMLTag(s string) bool      { return htmlTagRegex.MatchString(s) }

// jsonString stringifies a JSON-decoded value the way JS String() would for
// primitives — used for value:change displayLabel coercion.
func jsonString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		// JSON numbers come in as float64; render as JS Number.toString would.
		return jsonNumberString(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case nil:
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func jsonNumberString(f float64) string {
	// Render integers without trailing ".0".
	if f == math.Trunc(f) && !math.IsInf(f, 0) && math.Abs(f) < 1e21 {
		return jsonIntString(int64(f))
	}
	b, _ := json.Marshal(f)
	return string(b)
}

func jsonIntString(n int64) string {
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

func jsonEqual(a, b interface{}) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ja) == string(jb)
}

func ensureSliceOfMaps(s []map[string]interface{}) []map[string]interface{} {
	if s == nil {
		return []map[string]interface{}{}
	}
	return s
}
