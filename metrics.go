package pdiggerfactory

import (
	"math"
	"strconv"
	"strings"
)

// MetricSample is one sample of the Prometheus text exposition.
type MetricSample struct {
	Name   string
	Labels map[string]string
	Value  float64
}

// Metrics is the reply of GetMetrics: the text and its samples, in order.
type Metrics struct {
	Text    string
	Samples []MetricSample
}

// Find returns the samples of a metric whose labels include all the given ones.
func (m *Metrics) Find(name string, labels map[string]string) []MetricSample {
	var out []MetricSample
	for _, s := range m.Samples {
		if s.Name != name {
			continue
		}
		ok := true
		for k, v := range labels {
			if s.Labels[k] != v {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, s)
		}
	}
	return out
}

// ParseMetrics parses the Prometheus text format: "# " comment lines are skipped, a sample is
// name{label="value",...} value [timestamp]. Lines it does not understand are skipped.
func ParseMetrics(text string) *Metrics {
	m := &Metrics{Text: text}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		if s, ok := parseSample(line); ok {
			m.Samples = append(m.Samples, s)
		}
	}
	return m
}

func parseSample(line string) (MetricSample, bool) {
	s := MetricSample{Labels: map[string]string{}}
	i := strings.IndexAny(line, "{ \t")
	if i <= 0 {
		return s, false
	}
	s.Name = line[:i]
	rest := line[i:]
	if rest[0] == '{' {
		labels, after, ok := parseLabels(rest[1:])
		if !ok {
			return s, false
		}
		s.Labels = labels
		rest = after
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return s, false
	}
	v, ok := parseValue(fields[0])
	if !ok {
		return s, false
	}
	s.Value = v
	return s, true
}

// parseLabels reads label="value" pairs up to the closing brace and returns the rest of the line.
func parseLabels(s string) (map[string]string, string, bool) {
	labels := map[string]string{}
	for {
		s = strings.TrimLeft(s, " ,")
		if strings.HasPrefix(s, "}") {
			return labels, s[1:], true
		}
		eq := strings.IndexByte(s, '=')
		if eq <= 0 || eq+1 >= len(s) || s[eq+1] != '"' {
			return nil, "", false
		}
		name := strings.TrimSpace(s[:eq])
		var b strings.Builder
		j := eq + 2
		for ; j < len(s); j++ {
			c := s[j]
			if c == '\\' && j+1 < len(s) {
				j++
				switch s[j] {
				case 'n':
					b.WriteByte('\n')
				default:
					b.WriteByte(s[j])
				}
				continue
			}
			if c == '"' {
				break
			}
			b.WriteByte(c)
		}
		if j >= len(s) {
			return nil, "", false
		}
		labels[name] = b.String()
		s = s[j+1:]
	}
}

func parseValue(v string) (float64, bool) {
	switch v {
	case "+Inf", "Inf":
		return math.Inf(1), true
	case "-Inf":
		return math.Inf(-1), true
	case "NaN":
		return math.NaN(), true
	}
	f, err := strconv.ParseFloat(v, 64)
	return f, err == nil
}
