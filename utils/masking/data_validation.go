package masking

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"

	"github.com/tidwall/gjson"
)

// isJSON осуществляет проверку на то, является ли s - JSON объектом
func isJSON(b []byte) bool {
	return gjson.ValidBytes(b)
}

// isXML осуществляет проверку на то, является ли данные корректным XML,
func isXML(b []byte) bool {
	decoder := xml.NewDecoder(bytes.NewReader(b))
	for {
		_, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return false
		}
	}
	return true
}
