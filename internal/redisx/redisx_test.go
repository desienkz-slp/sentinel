package redisx

import (
	"bufio"
	"context"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRedis = server RESP minimal: AUTH, PING, SET NX EX, TTL.
type fakeRedis struct {
	ln       net.Listener
	mu       sync.Mutex
	data     map[string]time.Time
	password string
	conns    int
}

func newFake(t *testing.T, password string) *fakeRedis {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRedis{ln: ln, data: map[string]time.Time{}, password: password}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			f.mu.Lock()
			f.conns++
			f.mu.Unlock()
			go f.serve(c)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return f
}

func (f *fakeRedis) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	authed := f.password == ""
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		var n int
		if _, e := sscan(line, &n); e != nil {
			return
		}
		args := make([]string, 0, n)
		for i := 0; i < n; i++ {
			l, _ := r.ReadString('\n')
			var ln int
			sscan2(l, &ln)
			buf := make([]byte, ln+2)
			_, _ = readFull(r, buf)
			args = append(args, string(buf[:ln]))
		}
		cmd := strings.ToUpper(args[0])
		switch {
		case cmd == "AUTH":
			if args[1] == f.password {
				authed = true
				_, _ = c.Write([]byte("+OK\r\n"))
			} else {
				_, _ = c.Write([]byte("-ERR invalid password\r\n"))
			}
		case !authed:
			_, _ = c.Write([]byte("-NOAUTH Authentication required.\r\n"))
		case cmd == "PING":
			_, _ = c.Write([]byte("+PONG\r\n"))
		case cmd == "SET":
			f.mu.Lock()
			exp, ok := f.data[args[1]]
			if ok && time.Now().After(exp) {
				ok = false
			}
			if ok {
				f.mu.Unlock()
				_, _ = c.Write([]byte("$-1\r\n"))
				continue
			}
			f.data[args[1]] = time.Now().Add(time.Duration(atoi(args[5])) * time.Second)
			f.mu.Unlock()
			_, _ = c.Write([]byte("+OK\r\n"))
		case cmd == "TTL":
			f.mu.Lock()
			exp, ok := f.data[args[1]]
			f.mu.Unlock()
			if !ok {
				_, _ = c.Write([]byte(":-2\r\n"))
			} else {
				_, _ = c.Write([]byte(":" + itoa(int(time.Until(exp).Seconds())+1) + "\r\n"))
			}
		default:
			_, _ = c.Write([]byte("-ERR unknown\r\n"))
		}
	}
}

func TestPingDanAuth(t *testing.T) {
	f := newFake(t, "rahasia-uji")
	ok := New(f.ln.Addr().String(), "rahasia-uji")
	if err := ok.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	bad := New(f.ln.Addr().String(), "salah")
	err := bad.Ping(context.Background())
	if err == nil {
		t.Fatal("kata sandi salah harus ditolak")
	}
	if strings.Contains(err.Error(), "salah") || strings.Contains(err.Error(), "rahasia") {
		t.Fatalf("error membocorkan kata sandi: %v", err)
	}
	none := New(f.ln.Addr().String(), "")
	if err := none.Ping(context.Background()); err == nil {
		t.Fatal("tanpa kata sandi harus ditolak")
	}
}

func TestSetNXDanTTL(t *testing.T) {
	f := newFake(t, "")
	c := New(f.ln.Addr().String(), "")
	first, err := c.SetNX("k1", 120*time.Second)
	if err != nil || !first {
		t.Fatalf("pertama harus baru: %v %v", first, err)
	}
	second, _ := c.SetNX("k1", 120*time.Second)
	if second {
		t.Fatal("kedua harus duplikat")
	}
	ttl, _ := c.TTL("k1")
	if ttl < 100 || ttl > 121 {
		t.Fatalf("ttl = %d", ttl)
	}
}

func TestPulihSetelahKoneksiPutus(t *testing.T) {
	f := newFake(t, "")
	c := New(f.ln.Addr().String(), "")
	if err := c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	_ = c.conn.Close() // putuskan paksa
	c.mu.Unlock()
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("harus menyambung ulang: %v", err)
	}
}

func TestRedisMatiMengembalikanErrorBukanPanic(t *testing.T) {
	c := New("127.0.0.1:1", "RAHASIA-XYZ")
	if err := c.Ping(context.Background()); err == nil || strings.Contains(err.Error(), "RAHASIA-XYZ") {
		t.Fatalf("harus error tanpa bocor: %v", err)
	}
}

// Redis ASLI (dijalankan di server): NOC_REDIS_TEST_ADDR + NOC_REDIS_TEST_PASS.
func TestRedisAsli(t *testing.T) {
	addr := os.Getenv("NOC_REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("NOC_REDIS_TEST_ADDR tidak diset")
	}
	c := New(addr, os.Getenv("NOC_REDIS_TEST_PASS"))
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("ping asli: %v", err)
	}
	key := "noc:test:" + itoa(int(time.Now().UnixNano()%1e9))
	a, _ := c.SetNX(key, 30*time.Second)
	b, _ := c.SetNX(key, 30*time.Second)
	if !a || b {
		t.Fatalf("SetNX asli: %v %v", a, b)
	}
	if ttl, _ := c.TTL(key); ttl < 1 || ttl > 30 {
		t.Fatalf("kunci harus punya TTL: %d", ttl)
	}
	bad := New(addr, "salah-total")
	if err := bad.Ping(context.Background()); err == nil {
		t.Fatal("Redis asli harus menolak kata sandi salah")
	}
}
