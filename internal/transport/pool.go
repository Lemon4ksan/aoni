package transport

import (
	"context"
	"crypto/tls"
	"net"
	"sync"
	"time"

	"github.com/lemon4ksan/mach/client/h1"
	machhttp "github.com/lemon4ksan/mach/proto/http"
)

type Pool struct {
	conns map[string][]*h1.ClientConn
	mu    sync.Mutex

	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	TLSConfig    *tls.Config
	Dial         func(string) (net.Conn, error)
	DialDualStack bool
	DisableHeaderNamesNormalizing bool
}

func NewPool() *Pool {
	return &Pool{
		conns: make(map[string][]*h1.ClientConn),
	}
}

func (p *Pool) Do(req *machhttp.Request, res *machhttp.Response) error {
	addr := string(req.Host())
	if addr == "" {
		addr = string(req.URI().Host())
	}

	p.mu.Lock()
	var cc *h1.ClientConn
	if list := p.conns[addr]; len(list) > 0 {
		cc = list[len(list)-1]
		p.conns[addr] = list[:len(list)-1]
	}
	p.mu.Unlock()

	if cc == nil {
		dialer := p.Dial
		if dialer == nil {
			dialer = func(addr string) (net.Conn, error) {
				return net.Dial("tcp", addr)
			}
		}
		c, err := dialer(addr)
		if err != nil {
			return err
		}
		cc = h1.NewClientConn(c)
	}

	err := cc.Do(context.Background(), req, res)
	if err != nil {
		cc.Close()
		return err
	}

	if !res.ConnectionClose() {
		p.mu.Lock()
		p.conns[addr] = append(p.conns[addr], cc)
		p.mu.Unlock()
	} else {
		cc.Close()
	}

	return nil
}

func (p *Pool) DoPipeline(reqs []*machhttp.Request, resps []*machhttp.Response) error {
	for i := range reqs {
		if err := p.Do(reqs[i], resps[i]); err != nil {
			return err
		}
	}
	return nil
}

func (p *Pool) CloseIdleConnections() {
	p.mu.Lock()
	for _, list := range p.conns {
		for _, cc := range list {
			cc.Close()
		}
	}
	p.conns = make(map[string][]*h1.ClientConn)
	p.mu.Unlock()
}
