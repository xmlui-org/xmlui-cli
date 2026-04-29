package distillcmd

import "sort"

// DistillSummaryOutput is the compact analysis-oriented form of a
// distillation. It keeps the same step ordering while collapsing noisy nested
// payloads into brief, narration-friendly summaries.
type DistillSummaryOutput struct {
	Overview map[string]interface{}   `json:"overview"`
	Steps    []map[string]interface{} `json:"steps"`
}

// SummarizeDistillOutput converts the full distillation into a compact form
// that is easier for models (and humans) to analyze quickly.
func SummarizeDistillOutput(out DistillOutput) DistillSummaryOutput {
	steps := make([]map[string]interface{}, 0, len(out.Steps))
	actions := make([]string, 0, len(out.Steps))
	apiCallCount := 0

	for i, step := range out.Steps {
		if action, _ := step["action"].(string); action != "" {
			actions = append(actions, action)
		}
		summary := summarizeStep(i, step)
		if apis, ok := summary["api"].([]map[string]interface{}); ok {
			apiCallCount += len(apis)
		}
		steps = append(steps, summary)
	}

	return DistillSummaryOutput{
		Overview: map[string]interface{}{
			"stepCount":    len(steps),
			"actions":      actions,
			"apiCallCount": apiCallCount,
		},
		Steps: steps,
	}
}

func summarizeStep(index int, step map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{
		"index": index + 1,
	}

	if action, _ := step["action"].(string); action != "" {
		out["action"] = action
	}
	if target, ok := step["target"].(map[string]interface{}); ok {
		if compact := summarizeTarget(target); len(compact) > 0 {
			out["target"] = compact
		}
	}
	if fillValue, ok := step["fillValue"].(string); ok && fillValue != "" {
		out["fillValue"] = fillValue
	}
	if submenuParent, _ := step["submenuParent"].(string); submenuParent != "" {
		out["submenuParent"] = submenuParent
	}

	if awaitMap, ok := step["await"].(map[string]interface{}); ok {
		if apis := summarizeAPICalls(awaitMap["api"]); len(apis) > 0 {
			out["api"] = apis
		}
		if navigate, ok := awaitMap["navigate"].(map[string]interface{}); ok && len(navigate) > 0 {
			out["navigate"] = navigate
		}
	}

	if vcs := summarizeValueChanges(step["valueChanges"]); len(vcs) > 0 {
		out["valueChanges"] = vcs
	}
	if toasts := summarizeSliceOfMaps(step["toasts"]); len(toasts) > 0 {
		out["toasts"] = toasts
	}
	if modals := summarizeSliceOfMaps(step["modals"]); len(modals) > 0 {
		out["modals"] = modals
	}
	if diffs := summarizeNamedList(step["stateDiffs"], "path"); len(diffs) > 0 {
		out["stateDiffPaths"] = diffs
	}
	if dsChanges := summarizeSliceOfMaps(step["dataSourceChanges"]); len(dsChanges) > 0 {
		out["dataSourceChanges"] = dsChanges
	}
	if validation := summarizeValidation(step["validationErrors"]); len(validation) > 0 {
		out["validationErrors"] = validation
	}
	if binds := summarizeSliceOfMaps(step["dataBinds"]); len(binds) > 0 {
		out["dataBinds"] = binds
	}
	if traces := summarizeAppTraces(step["appTraces"]); len(traces) > 0 {
		out["appTraces"] = traces
	}

	return out
}

func summarizeTarget(target map[string]interface{}) map[string]interface{} {
	if target == nil {
		return nil
	}
	keys := []string{
		"component",
		"label",
		"ariaRole",
		"ariaName",
		"testId",
		"selectedPath",
		"key",
		"ctrlKey",
		"shiftKey",
		"metaKey",
		"altKey",
	}
	out := map[string]interface{}{}
	for _, key := range keys {
		if v, ok := target[key]; ok && v != nil {
			out[key] = v
		}
	}
	return out
}

func summarizeAPICalls(v interface{}) []map[string]interface{} {
	raw, ok := v.([]map[string]interface{})
	if !ok {
		rawIface, ok := v.([]interface{})
		if !ok {
			return nil
		}
		raw = make([]map[string]interface{}, 0, len(rawIface))
		for _, item := range rawIface {
			if m, ok := item.(map[string]interface{}); ok {
				raw = append(raw, m)
			}
		}
	}

	out := make([]map[string]interface{}, 0, len(raw))
	for _, api := range raw {
		item := map[string]interface{}{}
		for _, key := range []string{"method", "endpoint", "status"} {
			if v, ok := api[key]; ok && v != nil {
				item[key] = v
			}
		}
		if apiResult, ok := api["apiResult"].(map[string]interface{}); ok {
			item["result"] = summarizeAPIResult(apiResult)
		}
		out = append(out, item)
	}
	return out
}

func summarizeAPIResult(apiResult map[string]interface{}) map[string]interface{} {
	if apiResult == nil {
		return nil
	}
	out := map[string]interface{}{}
	for _, key := range []string{"type", "count", "keys", "values"} {
		if v, ok := apiResult[key]; ok && v != nil {
			out[key] = v
		}
	}
	return out
}

func summarizeValueChanges(v interface{}) []map[string]interface{} {
	raw := summarizeSliceOfMaps(v)
	if len(raw) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		compact := map[string]interface{}{}
		for _, key := range []string{"component", "componentLabel", "ariaName", "value", "files"} {
			if v, ok := item[key]; ok && v != nil {
				compact[key] = v
			}
		}
		out = append(out, compact)
	}
	return out
}

func summarizeValidation(v interface{}) []map[string]interface{} {
	raw := summarizeSliceOfMaps(v)
	if len(raw) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		compact := map[string]interface{}{}
		for _, key := range []string{"form", "errorCount", "errorFields"} {
			if v, ok := item[key]; ok && v != nil {
				compact[key] = v
			}
		}
		out = append(out, compact)
	}
	return out
}

func summarizeAppTraces(v interface{}) map[string]int {
	raw, _ := v.(map[string][]interface{})
	if raw == nil {
		rawIface, ok := v.(map[string]interface{})
		if !ok {
			return nil
		}
		raw = map[string][]interface{}{}
		for k, vv := range rawIface {
			if arr, ok := vv.([]interface{}); ok {
				raw[k] = arr
			}
		}
	}
	out := map[string]int{}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out[key] = len(raw[key])
	}
	return out
}

func summarizeNamedList(v interface{}, field string) []string {
	raw := summarizeSliceOfMaps(v)
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, _ := item[field].(string); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func summarizeSliceOfMaps(v interface{}) []map[string]interface{} {
	switch x := v.(type) {
	case []map[string]interface{}:
		return x
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(x))
		for _, item := range x {
			if m, ok := item.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}
