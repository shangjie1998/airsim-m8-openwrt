package gateway

import (
	"encoding/json"
	"time"
)

// Existing AirSIM wire metadata; optional and independent of call capability.
type batteryReading struct {
	Percent      int   `json:"percent"`
	Charging     bool  `json:"charging"`
	ObservedAtMS int64 `json:"observed_at_ms"`
}

// Do not let malformed optional metadata break identity/health, or refresh an
// old reading's timestamp merely because the client requested health again.
func validBattery(raw json.RawMessage, now time.Time) *batteryReading {
	var value struct {
		Percent      *int  `json:"percent"`
		Charging     *bool `json:"charging"`
		ObservedAtMS int64 `json:"observed_at_ms"`
	}
	if len(raw) > 256 || json.Unmarshal(raw, &value) != nil || value.Percent == nil || value.Charging == nil {
		return nil
	}
	if *value.Percent < 0 || *value.Percent > 100 || value.ObservedAtMS <= 0 || value.ObservedAtMS <= now.UnixMilli()-300_000 || value.ObservedAtMS > now.UnixMilli()+60_000 {
		return nil
	}
	return &batteryReading{Percent: *value.Percent, Charging: *value.Charging, ObservedAtMS: value.ObservedAtMS}
}
