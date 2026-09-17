package utils

import (
	"encoding/json"
)

func JsonEncode(data any) string {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	return string(jsonData)
}

func NormalizePayload(input any) any {
	if input == nil {
		return map[string]any{}
	}

	raw, err := json.Marshal(input)
	if err != nil {
		return input
	}

	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return input
	}

	return normalized
}

func MustJSON(value any) json.RawMessage {
	body, err := json.Marshal(value)
	if err != nil {
		return EmptyJSON()
	}
	return body
}

func EmptyJSON() json.RawMessage {
	return json.RawMessage(`{}`)
}
