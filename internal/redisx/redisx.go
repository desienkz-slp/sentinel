// Package redisx = klien Redis minimal (RESP2, hanya stdlib) untuk dedupe.
// Sengaja kecil: AUTH, PING, SET NX EX. Tidak menambah dependensi.
package redisx

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// Client satu koneksi yang dibuka ulang bila putus. Aman untuk goroutine.
type Client struct {
	addr, password string
	timeout        time.Duration
	mu             sync.Mutex
	conn           net.Conn
	rd             *bufio.Reader
}

func New(addr, password string) *Client {
	return &Client{addr: addr, password: password, timeout: 1500 * time.Millisecond}
}

// Redacted = alamat saja, tanpa kata sandi.
func (c *Client) Redacted() string { return c.addr }

func (c *Client) dial() error {
	d := net.Dialer{Timeout: c.timeout}
	conn, err := d.Dial("tcp", c.addr)
	if err != nil {
		return errors.New("redis tidak terjangkau")
	}
	c.conn, c.rd = conn, bufio.NewReader(conn)
	if c.password != "" {
		if _, err := c.cmdLocked("AUTH", c.password); err != nil {
			c.closeLocked()
			return errors.New("redis menolak autentikasi")
		}
	}
	return nil
}

func (c *Client) closeLocked() {
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.conn, c.rd = nil, nil
}

// Close menutup koneksi.
func (c *Client) Close() { c.mu.Lock(); c.closeLocked(); c.mu.Unlock() }

func (c *Client) cmdLocked(args ...string) (string, error) {
	_ = c.conn.SetDeadline(time.Now().Add(c.timeout))
	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, a := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(a), a)
	}
	if _, err := c.conn.Write([]byte(b.String())); err != nil {
		return "", err
	}
	line, err := c.rd.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return "", errors.New("balasan kosong")
	}
	switch line[0] {
	case '+', ':':
		return line[1:], nil
	case '-':
		return "", errors.New("redis: " + line[1:])
	case '$':
		if line == "$-1" {
			return "", nil
		}
		var n int
		fmt.Sscanf(line[1:], "%d", &n)
		buf := make([]byte, n+2)
		if _, err := ioReadFull(c.rd, buf); err != nil {
			return "", err
		}
		return string(buf[:n]), nil
	}
	return "", errors.New("balasan tak dikenal")
}

func ioReadFull(r *bufio.Reader, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := r.Read(buf[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// do menjalankan perintah; satu kali coba ulang bila koneksi putus.
func (c *Client) do(args ...string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for attempt := 0; attempt < 2; attempt++ {
		if c.conn == nil {
			if err := c.dial(); err != nil {
				return "", err
			}
		}
		out, err := c.cmdLocked(args...)
		if err == nil {
			return out, nil
		}
		if strings.HasPrefix(err.Error(), "redis: ") { // galat logis, bukan koneksi
			return "", err
		}
		c.closeLocked()
	}
	return "", errors.New("redis tidak menjawab")
}

// Ping memeriksa koneksi.
func (c *Client) Ping(ctx context.Context) error {
	out, err := c.do("PING")
	if err != nil {
		return err
	}
	if out != "PONG" {
		return errors.New("balasan PING tak terduga")
	}
	return nil
}

// SetNX menyimpan key hanya bila belum ada, dengan TTL. true = baru dibuat.
func (c *Client) SetNX(key string, ttl time.Duration) (bool, error) {
	secs := int(ttl.Seconds())
	if secs < 1 {
		secs = 1
	}
	out, err := c.do("SET", key, "1", "NX", "EX", fmt.Sprint(secs))
	if err != nil {
		return false, err
	}
	return out == "OK", nil
}

// TTL mengembalikan sisa detik (-2 = tidak ada, -1 = tanpa kedaluwarsa).
func (c *Client) TTL(key string) (int, error) {
	out, err := c.do("TTL", key)
	if err != nil {
		return 0, err
	}
	var n int
	fmt.Sscanf(out, "%d", &n)
	return n, nil
}
