// Package missionhandler parses WPL mission format and validates delivery missions.
package missionhandler

import (
	"strconv"
	"strings"
	"time"
)

const mavCmdNavWaypoint = 16

// ParseWPL parses QGC WPL content into a mission map: mission_id, home, steps (each with lat, lon, alt_m, speed_mps, drop).
func ParseWPL(wplContent string, missionID string) (map[string]interface{}, string) {
	wplContent = strings.TrimSpace(wplContent)
	if wplContent == "" {
		return nil, "empty_or_invalid_wpl"
	}
	lines := strings.Split(wplContent, "\n")
	var nonEmpty []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			nonEmpty = append(nonEmpty, l)
		}
	}
	if len(nonEmpty) == 0 {
		return nil, "empty_wpl"
	}
	header := strings.ToUpper(nonEmpty[0])
	if !strings.HasPrefix(header, "QGC WPL") && !strings.Contains(header, "WPL") {
		return nil, "invalid_wpl_header"
	}
	var steps []map[string]interface{}
	var home map[string]interface{}
	for i := 1; i < len(nonEmpty); i++ {
		line := nonEmpty[i]
		parts := strings.Split(line, "\t")
		if len(parts) < 12 {
			parts = strings.Fields(line)
		}
		if len(parts) < 12 {
			return nil, "invalid_wpl_line_" + strconv.Itoa(i) + "_too_few_columns"
		}
		idx, errIdx := strconv.Atoi(strings.TrimSpace(parts[0]))
		cmd, errCmd := strconv.Atoi(strings.TrimSpace(parts[3]))
		lat, errLat := strconv.ParseFloat(strings.TrimSpace(parts[8]), 64)
		lon, errLon := strconv.ParseFloat(strings.TrimSpace(parts[9]), 64)
		alt, errAlt := strconv.ParseFloat(strings.TrimSpace(parts[10]), 64)
		if errIdx != nil || errCmd != nil || errLat != nil || errLon != nil || errAlt != nil {
			return nil, "invalid_wpl_line_" + strconv.Itoa(i) + "_parse_error"
		}
		current, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
		if idx == 0 && current == 1 {
			home = map[string]interface{}{"lat": lat, "lon": lon, "alt_m": alt}
			continue
		}
		if cmd != mavCmdNavWaypoint {
			continue
		}
		step := map[string]interface{}{
			"id":        "wp-" + pad3(len(steps)),
			"lat":       lat,
			"lon":       lon,
			"alt_m":     alt,
			"speed_mps": 5.0,
			"drop":      false,
		}
		steps = append(steps, step)
	}
	if len(steps) == 0 {
		return nil, "no_waypoints_in_wpl"
	}
	if home == nil {
		first := steps[0]
		home = map[string]interface{}{"lat": first["lat"], "lon": first["lon"], "alt_m": 0.0}
	}
	if missionID == "" {
		missionID = "wpl-" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	}
	stepsAsInterface := make([]interface{}, len(steps))
	for i, step := range steps {
		stepsAsInterface[i] = step
	}
	return map[string]interface{}{
		"mission_id": missionID,
		"home":       home,
		"steps":      stepsAsInterface,
	}, ""
}

func pad3(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 3 {
		s = "0" + s
	}
	return s
}