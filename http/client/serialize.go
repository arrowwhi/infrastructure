package httpclient

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strings"
)

var defaultJSONSerializer = json.Marshal
var defaultXMLSerializer = xml.Marshal
var defaultJSONDeserializer = json.Unmarshal
var defaultXMLDeserializer = xml.Unmarshal

func (r *Request) serializeBody() (data []byte, err error) {
	contentType := r.headers.Get("Content-Type")

	parts := strings.Split(contentType, ";")
	contentType = strings.ToLower(strings.TrimSpace(parts[0]))

	if serializer, found := r.client.serializers[contentType]; found {
		return serializer(r.body)
	}

	return r.body.([]byte), nil
}

func (r *Request) deserializeBody(body []byte, contentType string, target interface{}) error {
	parts := strings.Split(contentType, ";")
	contentType = strings.ToLower(strings.TrimSpace(parts[0]))

	if deserializer, found := r.client.deserializers[contentType]; found {
		return deserializer(body, target)
	}

	if bytesPtr, ok := target.(*[]byte); ok {
		*bytesPtr = body
		return nil
	}

	return fmt.Errorf("%w: %s", ErrNoDeserializerFound, contentType)
}
