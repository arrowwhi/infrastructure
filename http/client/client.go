package httpclient

import (
	"context"
	"net"
	"net/http"

	"go.opentelemetry.io/otel/metric"
)

var _ HTTPClient = (*client)(nil)

type HTTPClient interface {
	Get(ctx context.Context, path string) *Request
	Post(ctx context.Context, path string) *Request
	Put(ctx context.Context, path string) *Request
	Delete(ctx context.Context, path string) *Request
	Patch(ctx context.Context, path string) *Request

	R(ctx context.Context, method, path string) *Request
}

type client struct {
	client        *http.Client
	log           Logger
	headers       http.Header
	serializers   map[string]SerializerFunc
	deserializers map[string]DeserializerFunc

	retryCounter metric.Int64Counter

	config Config
}

func (c *client) Get(ctx context.Context, path string) *Request {
	return c.R(ctx, http.MethodGet, path)
}

func (c *client) Post(ctx context.Context, path string) *Request {
	return c.R(ctx, http.MethodPost, path)
}

func (c *client) Put(ctx context.Context, path string) *Request {
	return c.R(ctx, http.MethodPut, path)
}

func (c *client) Delete(ctx context.Context, path string) *Request {
	return c.R(ctx, http.MethodDelete, path)
}

func (c *client) Patch(ctx context.Context, path string) *Request {
	return c.R(ctx, http.MethodPatch, path)
}

func (c *client) R(ctx context.Context, method, path string) *Request {
	req := &Request{
		client:  c,
		method:  method,
		path:    path,
		headers: c.headers.Clone(),
		ctx:     ctx,
	}

	return req
}

func NewClient(cfg Config, opts ...Option) (HTTPClient, error) {

	tlsConfig, err := buildTLSConfig(cfg.TLS)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: cfg.DialTimeout,
		}).DialContext,
		TLSHandshakeTimeout:   cfg.TLSHandshakeTimeout,
		ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
		MaxIdleConns:          cfg.MaxIdleConns,
		IdleConnTimeout:       cfg.IdleConnTimeout,
		TLSClientConfig:       tlsConfig,
	}

	httpClient := &http.Client{
		Transport: instrumentTransport(cfg, transport),
	}

	retryCounter, err := newRetryCounter(cfg)
	if err != nil {
		return nil, err
	}

	cli := &client{
		client:        httpClient,
		config:        cfg,
		retryCounter:  retryCounter,
		headers:       make(http.Header),
		log:           DefaultLogger(),
		serializers:   make(map[string]SerializerFunc),
		deserializers: make(map[string]DeserializerFunc),
	}

	cli.serializers["application/json"] = defaultJSONSerializer
	cli.serializers["application/xml"] = defaultXMLSerializer
	cli.deserializers["application/json"] = defaultJSONDeserializer
	cli.deserializers["application/xml"] = defaultXMLDeserializer

	for _, opt := range opts {
		opt(cli)
	}

	return cli, nil
}
