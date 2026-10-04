package workspace

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Canonical tokens are a closed palette. Imported arbitrary CSS stays in source
// evidence and is never executed by the editor or portable HTML rendering.
var richForeground = map[string]string{"gray": "#60646c", "brown": "#8c5a3c", "orange": "#b75d00", "yellow": "#856900", "green": "#287442", "blue": "#2161a6", "purple": "#7642a8", "pink": "#ab3a78", "red": "#b73737"}
var richBackground = map[string]string{"gray": "#e9e9eb", "brown": "#efdfd1", "orange": "#ffe2c0", "yellow": "#fff2ae", "green": "#d9eedc", "blue": "#dceafa", "purple": "#eadff7", "pink": "#f8dfee", "red": "#f9dcdc"}
var rgbColor = regexp.MustCompile(`^rgb\((\d+),(\d+),(\d+)\)$`)

func richColorToken(value string, highlight bool) string {
	value = strings.ToLower(strings.Join(strings.Fields(value), ""))
	if value == "grey" {
		value = "gray"
	}
	if _, ok := richForeground[value]; ok {
		return value
	}
	if match := rgbColor.FindStringSubmatch(value); match != nil {
		rgb := []int{}
		for _, part := range match[1:] {
			n, err := strconv.Atoi(part)
			if err != nil || n > 255 {
				return ""
			}
			rgb = append(rgb, n)
		}
		value = fmt.Sprintf("#%02x%02x%02x", rgb[0], rgb[1], rgb[2])
	}
	if len(value) == 4 && strings.HasPrefix(value, "#") {
		value = "#" + strings.Repeat(value[1:2], 2) + strings.Repeat(value[2:3], 2) + strings.Repeat(value[3:4], 2)
	}
	palette := richForeground
	if highlight {
		palette = richBackground
	}
	for token, css := range palette {
		if value == css {
			return token
		}
	}
	return map[string]string{"#808080": "gray", "#a52a2a": "brown", "#ffa500": "orange", "#ffff00": "yellow", "#008000": "green", "#0000ff": "blue", "#800080": "purple", "#ffc0cb": "pink", "#ff0000": "red"}[value]
}
