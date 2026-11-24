package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrJSON = errors.New("CONFIG_JSON_INVALID")
var ErrDuplicateKey = errors.New("CONFIG_JSON_DUPLICATE_KEY")

type jsonReader map[string]*string

// Ordinal casing follows .NET's simple uppercase comparison, including its
// non-folding dotless i and long s. Dots and colons are never rewritten.
func ordinalKey(key string) string {
	return strings.Map(func(r rune) rune {
		if r == '\u0131' || r == '\u017f' {
			return r
		}
		return unicode.ToUpper(r)
	}, key)
}
func (r jsonReader) Lookup(key string) (string, bool) {
	v, ok := r[ordinalKey(key)]
	if !ok || v == nil {
		return "", false
	}
	return *v, true
}

// ParseJSON implements the .NET 10 JSON Configuration flatmap: colon paths,
// decimal array indexes, case-insensitive keys, nullable scalars, and empty
// container behavior. The resulting Reader is immutable and owns its values.
func ParseJSON(input []byte) (Reader, error) {
	data, err := configurationJSON(input)
	if err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrJSON
	}
	result := jsonReader{}
	var value func(string, int) error
	var object func(string, int, bool) error
	object = func(path string, depth int, root bool) error {
		if depth > 64 {
			return ErrJSON
		}
		empty := true
		for d.More() {
			empty = false
			token, err := d.Token()
			if err != nil {
				return ErrJSON
			}
			name, ok := token.(string)
			if !ok {
				return ErrJSON
			}
			child := name
			if !root {
				child = path + ":" + name
			}
			if err = value(child, depth+1); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return ErrJSON
		}
		if empty && !root {
			result[ordinalKey(path)] = nil
		}
		return nil
	}
	value = func(path string, depth int) error {
		token, err := d.Token()
		if err != nil {
			return ErrJSON
		}
		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{':
				return object(path, depth, false)
			case '[':
				if depth > 64 {
					return ErrJSON
				}
				index := 0
				for d.More() {
					if err = value(path+":"+strconv.Itoa(index), depth+1); err != nil {
						return err
					}
					index++
				}
				end, err := d.Token()
				if err != nil || end != json.Delim(']') {
					return ErrJSON
				}
				if index == 0 {
					empty := ""
					result[ordinalKey(path)] = &empty
				}
				return nil
			default:
				return ErrJSON
			}
		}
		key := ordinalKey(path)
		if _, exists := result[key]; exists {
			return ErrDuplicateKey
		}
		var text string
		switch v := token.(type) {
		case nil:
			result[key] = nil
			return nil
		case string:
			text = v
		case json.Number:
			text = v.String()
		case bool:
			if v {
				text = "True"
			} else {
				text = "False"
			}
		default:
			return ErrJSON
		}
		result[key] = &text
		return nil
	}
	if err = object("", 1, true); err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, ErrJSON
	}
	return result, nil
}

// Preserve string bytes while skipping comments and permitted trailing commas.
func configurationJSON(input []byte) ([]byte, error) {
	data := bytes.Clone(bytes.TrimPrefix(input, []byte{0xef, 0xbb, 0xbf}))
	if !utf8.Valid(data) {
		return nil, ErrJSON
	}
	quoted, escaped := false, false
	for i := 0; i < len(data); i++ {
		if quoted {
			if escaped {
				escaped = false
				continue
			}
			if data[i] == '\\' {
				escaped = true
			} else if data[i] == '"' {
				quoted = false
			}
			continue
		}
		if data[i] == '"' {
			quoted = true
			continue
		}
		if data[i] != '/' || i+1 == len(data) {
			continue
		}
		switch data[i+1] {
		case '/':
			for ; i < len(data) && data[i] != '\n'; i++ {
				data[i] = ' '
			}
			i--
		case '*':
			data[i], data[i+1] = ' ', ' '
			i += 2
			closed := false
			for ; i < len(data); i++ {
				if data[i] == '*' && i+1 < len(data) && data[i+1] == '/' {
					data[i], data[i+1] = ' ', ' '
					i++
					closed = true
					break
				}
				if data[i] != '\n' && data[i] != '\r' {
					data[i] = ' '
				}
			}
			if !closed {
				return nil, ErrJSON
			}
		}
	}
	quoted, escaped = false, false
	for i, b := range data {
		if quoted {
			if escaped {
				escaped = false
				continue
			}
			if b == '\\' {
				escaped = true
			} else if b == '"' {
				quoted = false
			}
			continue
		}
		if b == '"' {
			quoted = true
			continue
		}
		if b != ',' {
			continue
		}
		before, after := i-1, i+1
		for before >= 0 && bytes.ContainsRune([]byte(" \t\r\n"), rune(data[before])) {
			before--
		}
		for after < len(data) && bytes.ContainsRune([]byte(" \t\r\n"), rune(data[after])) {
			after++
		}
		if before >= 0 && after < len(data) && !bytes.ContainsRune([]byte("[{,:"), rune(data[before])) && (data[after] == ']' || data[after] == '}') {
			data[i] = ' '
		}
	}
	return data, nil
}
