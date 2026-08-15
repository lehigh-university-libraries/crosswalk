// Package strictjson validates reviewed JSON trust material before ordinary
// typed decoding. encoding/json otherwise accepts duplicate object member
// names and silently keeps the last value.
package strictjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// RejectDuplicateNames reports duplicate member names at every object depth.
// It also verifies that input is exactly one syntactically valid JSON value.
func RejectDuplicateNames(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanValue(decoder, "$"); err != nil {
		return err
	}
	if token, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("unexpected token %v after top-level value", token)
	}
	return nil
}

func scanValue(decoder *json.Decoder, path string) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, structured := token.(json.Delim)
	if !structured {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object member at %s is not a string", path)
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON member %q at %s", key, path)
			}
			seen[key] = struct{}{}
			if err := scanValue(decoder, path+"."+key); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return fmt.Errorf("object at %s has invalid closing token %v", path, closing)
		}
	case '[':
		index := 0
		for decoder.More() {
			if err := scanValue(decoder, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
			index++
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return fmt.Errorf("array at %s has invalid closing token %v", path, closing)
		}
	default:
		return fmt.Errorf("unexpected delimiter %q at %s", delimiter, path)
	}
	return nil
}
