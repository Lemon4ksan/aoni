package fast

import (
	"context"
	"errors"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/transport"
	"github.com/lemon4ksan/foundation/generic"
	machhttp "github.com/lemon4ksan/mach/proto/http"
)

type Client struct {
	engine *transport.Pool
	cfg    aoni.Config
}

func NewClient(opts ...aoni.ClientOption) *Client {
	c := aoni.Config{}

	generic.ApplyOptions(&c, opts...)

	return &Client{
		engine: transport.NewPool(),
		cfg:    c,
	}
}

func (c *Client) Engine() *transport.Pool {
	return c.engine
}

func (c *Client) Do(req core.Request) (core.Response, error) {
	fastReq, ok := req.(*Request)
	if !ok {
		return nil, errors.New("fast: invalid request type, expected *fast.Request")
	}

	fastRes := NewResponse(nil)

	ctx := req.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	trailers, err, autoReleased := c.executeWithRedirects(ctx, fastReq.req, fastRes.resp)

	if err != nil {
		if !autoReleased {
			fastRes.Release()
		}
		return nil, err
	}

	fastRes.SetTrailers(trailers)

	return fastRes, nil
}

func (c *Client) execute(fastReq *machhttp.Request, fastRes *machhttp.Response) (map[string][]string, error, bool) {
	err := c.engine.Do(fastReq, fastRes)
	return nil, err, false
}
