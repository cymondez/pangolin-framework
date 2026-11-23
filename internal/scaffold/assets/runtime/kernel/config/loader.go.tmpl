package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Rule validates one startup value. Errors contain the key, never the value.
type Rule struct {
	Key      string
	Required bool
	Validate func(string) error
}

type loader struct{ rules []Rule }
type snapshot map[string]string

func (s snapshot) Lookup(key string) (string, bool) { v, ok := s[key]; return v, ok }

// NewLoader seals a copy of four named layers: defaults, file, environment, cli.
func NewLoader(rules []Rule) Loader { return &loader{rules: append([]Rule(nil), rules...)} }
func (l *loader) Load(sources []Source) (Reader, error) {
	layers := map[string]map[string]string{}
	for _, source := range sources {
		switch source.Name {
		case "defaults", "file", "environment", "cli":
		default:
			return nil, fmt.Errorf("unknown configuration source %q", source.Name)
		}
		if _, ok := layers[source.Name]; ok {
			return nil, fmt.Errorf("duplicate configuration source %s", source.Name)
		}
		layers[source.Name] = source.Values
	}
	values := snapshot{}
	for _, name := range []string{"defaults", "file", "environment", "cli"} {
		for k, v := range layers[name] {
			if strings.TrimSpace(k) == "" {
				return nil, fmt.Errorf("empty configuration key")
			}
			values[k] = v
		}
	}
	for _, rule := range l.rules {
		v, ok := values[rule.Key]
		if (!ok || v == "") && rule.Required {
			return nil, fmt.Errorf("missing configuration %s", rule.Key)
		}
		if ok && rule.Validate != nil {
			if err := rule.Validate(v); err != nil {
				return nil, fmt.Errorf("invalid configuration %s", rule.Key)
			}
		}
	}
	return values, nil
}

// StartupRules are the mandatory kernel settings; adapters add their own rules.
func StartupRules() []Rule {
	positiveDuration := func(v string) error {
		d, e := time.ParseDuration(v)
		if e != nil || d <= 0 {
			return fmt.Errorf("positive duration required")
		}
		return nil
	}
	positiveInt := func(v string) error {
		n, e := strconv.Atoi(v)
		if e != nil || n <= 0 {
			return fmt.Errorf("positive integer required")
		}
		return nil
	}
	rules := []Rule{}
	for _, key := range []string{"uow.default_timeout", "uow.cleanup_timeout", "uow.hook_timeout"} {
		rules = append(rules, Rule{key, true, positiveDuration})
	}
	return append(rules, Rule{"uow.max_requires_new_depth", true, positiveInt})
}

// URL validates a configured adapter URL without including credentials in errors.
func URL(schemes ...string) func(string) error {
	return func(s string) error {
		u, e := url.Parse(s)
		if e == nil && u.Host != "" {
			for _, scheme := range schemes {
				if u.Scheme == scheme {
					return nil
				}
			}
		}
		return fmt.Errorf("invalid URL")
	}
}
