package pdiggerfactory

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Defaults of the client options.
const (
	DefaultTimeout        = 10 * time.Second
	DefaultConnectTimeout = 5 * time.Second
	DefaultRetries        = 3
)

// DefaultBackoff returns the retry and reconnect backoff of a client made without WithBackoff: base 250 ms, factor
// 2, at most 5 s, full jitter.
func DefaultBackoff() Backoff {
	return Backoff{Base: 250 * time.Millisecond, Factor: 2, Max: 5 * time.Second}
}

// Backoff is an exponential backoff with full jitter: the wait before retry n (0-based) is a uniform random
// duration in [0, min(Max, Base*Factor^n)).
type Backoff struct {
	Base   time.Duration
	Factor float64
	Max    time.Duration
}

// Delay is the wait before retry attempt (0-based) for a random number r in [0, 1). A Base of 0 gives 0 at every
// attempt.
func (b Backoff) Delay(attempt int, r float64) time.Duration {
	if b.Base == 0 { // 0 * Factor^attempt would be 0 * +Inf, NaN, once the power overflows
		return 0
	}
	d := float64(b.Base) * math.Pow(b.Factor, float64(attempt))
	if d > float64(b.Max) {
		d = float64(b.Max)
	}
	return time.Duration(r * d)
}

// Client talks to one pDigger Factory server. It is safe for concurrent use.
type Client struct {
	base      *url.URL
	hc        *http.Client
	timeout   time.Duration
	retries   int
	backoff   Backoff
	headers   http.Header
	token     string
	userAgent string
	observe   bool
	clock     Clock

	randMu sync.Mutex
	rand   func() float64

	api     *API
	streams *Streams
	session *Session
}

type clientConfig struct {
	timeout        time.Duration
	connectTimeout time.Duration
	retries        int
	backoff        Backoff
	headers        http.Header
	token          string
	uaSuffix       string
	hc             *http.Client
	transport      http.RoundTripper
	observe        bool
	clock          Clock
	rnd            *rand.Rand
}

// Option configures a Client.
type Option func(*clientConfig)

// WithTimeout sets the request timeout (default 10 s): the whole of a request for ordinary calls, the wait for the
// reply headers for streams.
func WithTimeout(d time.Duration) Option { return func(c *clientConfig) { c.timeout = d } }

// WithConnectTimeout sets the TCP connect timeout of the default transport (default 5 s).
func WithConnectTimeout(d time.Duration) Option {
	return func(c *clientConfig) { c.connectTimeout = d }
}

// WithRetries sets how many times a failed request is retried (default 3; 0 disables retries).
func WithRetries(n int) Option { return func(c *clientConfig) { c.retries = n } }

// WithBackoff sets the retry and reconnect backoff (default DefaultBackoff()).
func WithBackoff(b Backoff) Option { return func(c *clientConfig) { c.backoff = b } }

// WithRand sets the random source of the backoff jitter; a seeded source makes the waits deterministic.
func WithRand(r *rand.Rand) Option { return func(c *clientConfig) { c.rnd = r } }

// WithHeader adds a header to every request.
func WithHeader(key, value string) Option {
	return func(c *clientConfig) { c.headers.Add(key, value) }
}

// WithBearerToken sends "Authorization: Bearer <token>" (the 20.20 unit ignores it; a deployment may require it).
func WithBearerToken(token string) Option { return func(c *clientConfig) { c.token = token } }

// WithUserAgentSuffix appends a suffix to the User-Agent "pdigger-factory-sdk-go/<version>".
func WithUserAgentSuffix(s string) Option { return func(c *clientConfig) { c.uaSuffix = s } }

// WithHTTPClient sets the HTTP client (its Timeout should be zero: streams are long-lived).
func WithHTTPClient(hc *http.Client) Option { return func(c *clientConfig) { c.hc = hc } }

// WithTransport sets the HTTP transport of the default HTTP client (factorytest.Fake.Transport() for tests).
func WithTransport(rt http.RoundTripper) Option { return func(c *clientConfig) { c.transport = rt } }

// WithObserve sets the default of the observe flag of streams: true does not count this client as a viewer, so an
// idle unit stays idle. The default is false: an integration that wants events keeps the interpreter running.
func WithObserve(observe bool) Option { return func(c *clientConfig) { c.observe = observe } }

// WithClock sets the clock of backoff waits, backfill and the session clock (default SystemClock).
func WithClock(clk Clock) Option { return func(c *clientConfig) { c.clock = clk } }

// New makes a client for the server at baseURL, such as "http://factory-orin.local:8120".
func New(baseURL string, opts ...Option) (*Client, error) {
	cfg := clientConfig{
		timeout:        DefaultTimeout,
		connectTimeout: DefaultConnectTimeout,
		retries:        DefaultRetries,
		backoff:        DefaultBackoff(),
		headers:        http.Header{},
		clock:          SystemClock{},
	}
	for _, o := range opts {
		o(&cfg)
	}
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, &Error{Op: "new", Code: CodeInvalidParam, Field: "base_url",
			Message: fmt.Sprintf("base URL %q must be http(s)://host[:port]", baseURL), Err: err}
	}
	c := &Client{
		base:      u,
		timeout:   cfg.timeout,
		retries:   cfg.retries,
		backoff:   cfg.backoff,
		headers:   cfg.headers,
		token:     cfg.token,
		userAgent: UserAgent,
		observe:   cfg.observe,
		clock:     cfg.clock,
		rand:      rand.Float64,
	}
	if cfg.uaSuffix != "" {
		c.userAgent += " " + cfg.uaSuffix
	}
	if cfg.rnd != nil {
		r := cfg.rnd
		c.rand = r.Float64
	}
	switch {
	case cfg.hc != nil:
		c.hc = cfg.hc
	default:
		rt := cfg.transport
		if rt == nil {
			rt = defaultTransport(cfg.connectTimeout)
		}
		c.hc = &http.Client{Transport: rt}
	}
	c.api = &API{c: c}
	c.streams = &Streams{c: c}
	c.session = &Session{c: c}
	return c, nil
}

func defaultTransport(connect time.Duration) *http.Transport {
	d := &net.Dialer{Timeout: connect, KeepAlive: 30 * time.Second}
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           d.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}

// BaseURL is the server's base URL.
func (c *Client) BaseURL() string { return c.base.String() }

// API is the low level: one method per HTTP operation.
func (c *Client) API() *API { return c.api }

// Streams is the mid level: frames, long-poll and raw packets with resume.
func (c *Client) Streams() *Streams { return c.streams }

// Session is the high-level session helper: watch a source, seek, pause, play, focus, the session clock.
func (c *Client) Session() *Session { return c.session }

// jitter returns a random number in [0, 1) from the client's source.
func (c *Client) jitter() float64 {
	c.randMu.Lock()
	defer c.randMu.Unlock()
	return c.rand()
}

// backoffDelay is the wait before retry attempt (0-based).
func (c *Client) backoffDelay(attempt int) time.Duration {
	return c.backoff.Delay(attempt, c.jitter())
}

// sleep waits on the client's clock.
func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	return c.clock.Sleep(ctx, d)
}
