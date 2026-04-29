package distillcmd

import "testing"

func TestSummarizeResultCompactsNestedArrays(t *testing.T) {
	result := map[string]interface{}{
		"weather": []interface{}{
			map[string]interface{}{
				"date": "2026-04-29",
				"hourly": []interface{}{
					map[string]interface{}{"time": "0", "tempC": "12"},
					map[string]interface{}{"time": "300", "tempC": "10"},
				},
			},
		},
	}

	got := summarizeResult(result)
	values, _ := got["values"].(map[string]interface{})
	weather, _ := values["weather"].(map[string]interface{})
	if weather["type"] != "array" {
		t.Fatalf("weather.type = %v, want array", weather["type"])
	}
	if weather["count"] != 1 {
		t.Fatalf("weather.count = %v, want 1", weather["count"])
	}
	sample, _ := weather["sample"].(map[string]interface{})
	hourly, _ := sample["hourly"].(map[string]interface{})
	if hourly["type"] != "array" {
		t.Fatalf("hourly.type = %v, want array", hourly["type"])
	}
	if hourly["count"] != 2 {
		t.Fatalf("hourly.count = %v, want 2", hourly["count"])
	}
}

func TestSummarizeResultRemainsGeneric(t *testing.T) {
	result := map[string]interface{}{
		"current_condition": []interface{}{
			map[string]interface{}{
				"temp_C": "23",
				"temp_F": "73",
				"weatherDesc": []interface{}{
					map[string]interface{}{"value": "Sunny"},
				},
			},
		},
		"nearest_area": []interface{}{
			map[string]interface{}{
				"areaName": []interface{}{map[string]interface{}{"value": "Santa Rosa"}},
				"region":   []interface{}{map[string]interface{}{"value": "California"}},
				"country":  []interface{}{map[string]interface{}{"value": "United States of America"}},
			},
		},
		"weather": []interface{}{
			map[string]interface{}{"date": "2026-04-29"},
			map[string]interface{}{"date": "2026-04-30"},
		},
	}

	got := summarizeResult(result)
	if _, ok := got["summary"]; ok {
		t.Fatalf("summarizeResult should stay generic and not add domain-specific summary")
	}
	values, _ := got["values"].(map[string]interface{})
	if _, ok := values["weather"]; !ok {
		t.Fatalf("expected compact generic values to retain weather key")
	}
}

func TestSummarizeDistillOutput(t *testing.T) {
	out := DistillOutput{
		Steps: []map[string]interface{}{
			{
				"action": "startup",
				"await": map[string]interface{}{
					"api": []map[string]interface{}{
						{
							"method":   "GET",
							"endpoint": "https://wttr.in/Santa Rosa, CA",
							"apiResult": map[string]interface{}{
								"type": "snapshot",
								"keys": []string{"current_condition", "weather"},
								"values": map[string]interface{}{
									"weather": map[string]interface{}{
										"type":  "array",
										"count": 3,
									},
								},
							},
						},
					},
				},
			},
			{
				"action":    "fill",
				"fillValue": "toronto ca",
				"target": map[string]interface{}{
					"component": "TextBox",
					"ariaRole":  "textbox",
					"ariaName":  "City",
				},
			},
		},
	}

	got := SummarizeDistillOutput(out)
	if got.Overview["stepCount"] != 2 {
		t.Fatalf("stepCount = %v, want 2", got.Overview["stepCount"])
	}
	if got.Overview["apiCallCount"] != 1 {
		t.Fatalf("apiCallCount = %v, want 1", got.Overview["apiCallCount"])
	}
	if got.Steps[1]["fillValue"] != "toronto ca" {
		t.Fatalf("fillValue = %v, want toronto ca", got.Steps[1]["fillValue"])
	}
	api, _ := got.Steps[0]["api"].([]map[string]interface{})
	result, _ := api[0]["result"].(map[string]interface{})
	values, _ := result["values"].(map[string]interface{})
	weather, _ := values["weather"].(map[string]interface{})
	if weather["count"] != 3 {
		t.Fatalf("summary api weather.count = %v, want 3", weather["count"])
	}
}
