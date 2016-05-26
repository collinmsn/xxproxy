package proxy

import (
	"errors"
	"net"
	"sync"
	"time"

	resp "github.com/collinmsn/stvpresp"
	log "github.com/ngaut/logging"
	"gopkg.in/fatih/pool.v2"
)

type ConnPool struct {
	pools        map[string]pool.Pool
	maxIdle      int
	connTimeout  time.Duration
	mu           sync.Mutex
	sendReadOnly bool
}

func NewConnPool(maxIdle int, connTimeout time.Duration, sendReadOnly bool) *ConnPool {
	p := &ConnPool{
		pools:        make(map[string]pool.Pool),
		maxIdle:      maxIdle,
		connTimeout:  connTimeout,
		sendReadOnly: sendReadOnly,
	}
	return p
}

func (cp *ConnPool) GetConn(server string) (net.Conn, error) {
	var err error
	cp.mu.Lock()
	p := cp.pools[server]
	// create a pool is quite cheap and will not triggered many times
	if p == nil {
		p, err = pool.NewChannelPool(0, cp.maxIdle, func() (net.Conn, error) {
			return cp.postConnect(net.DialTimeout("tcp", server, cp.connTimeout))
		})
		if err != nil {
			log.Fatal(err)
		}
		cp.pools[server] = p
	}
	cp.mu.Unlock()
	return p.Get()
}

func (cp *ConnPool) postConnect(conn net.Conn, err error) (net.Conn, error) {
	if err != nil || !cp.sendReadOnly {
		return conn, err
	}
	defer func() {
		if err != nil {
			conn.Close()
			conn = nil
		}
	}()

	if _, err = conn.Write(resp.NewCommand("READONLY")); err != nil {
		log.Error("write READONLY failed", conn.RemoteAddr().String(), err)
		return conn, err
	}

	reader := resp.NewReader(conn)
	obj, err := reader.ReadObject()
	if err != nil {
		log.Error("read READONLY resp failed", conn.RemoteAddr().String(), err)
		return conn, err
	}

	if buf := obj.Raw(); len(buf) > 0 && buf[0] == resp.ERROR_PREFIX {
		log.Error("READONLY resp is not OK", conn.RemoteAddr().String())
		err = errors.New("post connect error: READONLY resp is not OK")
	}
	return conn, err
}

func (cp *ConnPool) Remove(server string) {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	p := cp.pools[server]
	if p != nil {
		p.Close()
		delete(cp.pools, server)
	}
}
