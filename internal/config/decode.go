package config

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// MaxBytes bounds configuration parsing, including candidates supplied over HTTP.
const MaxBytes = 1 << 20

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

func (d *Duration) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return errors.New("duration must be a string")
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return errors.New("invalid duration")
	}
	*d = Duration(parsed)
	return nil
}

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.ShortTag() != "!!str" {
		return errors.New("duration must be a string")
	}
	parsed, err := time.ParseDuration(node.Value)
	if err != nil {
		return errors.New("invalid duration")
	}
	*d = Duration(parsed)
	return nil
}

// Decode accepts exactly one strict YAML or JSON document and applies defaults.
// It deliberately does not read environment variables or resolve credentials.
func Decode(data []byte) (Config, error) {
	if len(data) > MaxBytes {
		return Config{}, errors.New("configuration exceeds size limit")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return Config{}, fmt.Errorf("configuration document: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("configuration must contain exactly one document")
	}
	if len(document.Content) != 1 {
		return Config{}, errors.New("configuration document must be an object")
	}
	if err := checkNode(document.Content[0], reflect.TypeFor[Document](), "", 0); err != nil {
		return Config{}, err
	}
	cfg := Document{
		Server:  Server{Listen: ":8080"},
		Scan:    Scan{Interval: Duration(10 * time.Minute), Timeout: Duration(2 * time.Minute)},
		Sources: map[SourceName]Source{},
		Filters: map[FilterName]FilterSpec{},
	}
	strict := yaml.NewDecoder(bytes.NewReader(data))
	strict.KnownFields(true)
	if err := strict.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("configuration document: %w", err)
	}
	return Parse(cfg)
}

// checkNode preserves field paths, rejects duplicate keys and disallows aliases
// and nulls so they cannot silently erase or default conditions.
func checkNode(node *yaml.Node, typ reflect.Type, path string, depth int) error {
	if depth > 32 {
		return fmt.Errorf("%s: configuration nesting exceeds limit", path)
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if node.Kind == yaml.AliasNode || node.ShortTag() == "!!null" {
		return fmt.Errorf("%s: aliases and null values are not supported", path)
	}
	if typ == reflect.TypeFor[Duration]() {
		var value Duration
		if err := value.UnmarshalYAML(node); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		return nil
	}
	switch typ.Kind() {
	case reflect.Struct, reflect.Map:
		if node.Kind != yaml.MappingNode {
			return fmt.Errorf("%s: expected an object", path)
		}
		fields := make(map[string]reflect.Type)
		if typ.Kind() == reflect.Struct {
			for i := 0; i < typ.NumField(); i++ {
				field := typ.Field(i)
				fields[strings.Split(field.Tag.Get("yaml"), ",")[0]] = field.Type
			}
		}
		seen := make(map[string]bool)
		for i := 0; i < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			child := key.Value
			if path != "" {
				child = path + "." + child
			}
			if key.Kind != yaml.ScalarNode || key.ShortTag() != "!!str" {
				return fmt.Errorf("%s: object keys must be strings", child)
			}
			if seen[key.Value] {
				return fmt.Errorf("%s: duplicate key", child)
			}
			seen[key.Value] = true
			var next reflect.Type
			if typ.Kind() == reflect.Map {
				next = typ.Elem()
			} else {
				var exists bool
				next, exists = fields[key.Value]
				if !exists {
					return fmt.Errorf("%s: unknown field", child)
				}
			}
			if err := checkNode(value, next, child, depth+1); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if node.Kind != yaml.SequenceNode {
			return fmt.Errorf("%s: expected a list", path)
		}
		for i, child := range node.Content {
			if err := checkNode(child, typ.Elem(), fmt.Sprintf("%s[%d]", path, i), depth+1); err != nil {
				return err
			}
		}
	case reflect.String:
		if node.Kind != yaml.ScalarNode || node.ShortTag() != "!!str" {
			return fmt.Errorf("%s: expected a string", path)
		}
	case reflect.Int:
		if node.Kind != yaml.ScalarNode || node.ShortTag() != "!!int" {
			return fmt.Errorf("%s: expected an integer", path)
		}
	default:
		return fmt.Errorf("%s: unsupported field type", path)
	}
	return nil
}
