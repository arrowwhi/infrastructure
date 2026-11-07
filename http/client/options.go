package httpclient

import "net/http"

type Option func(*client)

func WithLogger(logger Logger) Option {
	return func(c *client) {
		c.log = logger
	}
}

func WithHeader(key, value string) Option {
	return func(c *client) {
		c.headers.Add(key, value)
	}
}

func WithHeaders(headers http.Header) Option {
	return func(c *client) {
		for key, values := range headers {
			for _, value := range values {
				c.headers.Add(key, value)
			}
		}
	}
}

func WithSerializer(contentType string, serializer SerializerFunc) Option {
	return func(c *client) {
		c.serializers[contentType] = serializer
	}
}

func WithDeserializer(contentType string, deserializer DeserializerFunc) Option {
	return func(c *client) {
		c.deserializers[contentType] = deserializer
	}
}
