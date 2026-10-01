package cache

import (
	"testing"
	"time"
)

func TestSetGet(t *testing.T) {
	c := New(time.Minute)
	c.Set("k", "v")
	if v, ok := c.Get("k"); !ok || v != "v" {
		t.Errorf("Get(k) = %v ok=%v, mau v true", v, ok)
	}
}

func TestExpiry(t *testing.T) {
	c := New(10 * time.Millisecond)
	c.Set("k", "v")
	time.Sleep(20 * time.Millisecond)
	if _, ok := c.Get("k"); ok {
		t.Error("entry kedaluwarsa harus hilang")
	}
}

func TestGetOrSetComputesOnce(t *testing.T) {
	c := New(time.Minute)
	calls := 0
	fn := func() (any, error) {
		calls++
		return "computed", nil
	}
	v1, err := c.GetOrSet("k", fn)
	if err != nil || v1 != "computed" {
		t.Fatalf("GetOrSet pertama = %v err=%v", v1, err)
	}
	// Kedua kali: dari cache, fn tidak dipanggil lagi.
	v2, _ := c.GetOrSet("k", fn)
	if v2 != "computed" {
		t.Errorf("GetOrSet kedua = %v", v2)
	}
	if calls != 1 {
		t.Errorf("fn dipanggil %dx, mau 1 (cache hit)", calls)
	}
}

func TestGetOrSetErrorNotCached(t *testing.T) {
	c := New(time.Minute)
	fn := func() (any, error) { return nil, errBoom }
	if _, err := c.GetOrSet("k", fn); err == nil {
		t.Error("error harus diteruskan")
	}
	if _, ok := c.Get("k"); ok {
		t.Error("hasil error tidak boleh di-cache")
	}
}

func TestDeleteAndClear(t *testing.T) {
	c := New(time.Minute)
	c.Set("a", 1)
	c.Set("b", 2)
	c.Delete("a")
	if _, ok := c.Get("a"); ok {
		t.Error("a harus terhapus")
	}
	c.Clear()
	if c.Len() != 0 {
		t.Errorf("Len setelah Clear = %d, mau 0", c.Len())
	}
}

func TestLenCountsOnlyValid(t *testing.T) {
	c := New(10 * time.Millisecond)
	c.Set("a", 1)
	c.Set("b", 2)
	if c.Len() != 2 {
		t.Errorf("Len = %d, mau 2", c.Len())
	}
	time.Sleep(20 * time.Millisecond)
	if c.Len() != 0 {
		t.Errorf("Len setelah kedaluwarsa = %d, mau 0", c.Len())
	}
}

func TestTTL(t *testing.T) {
	c := New(5 * time.Second)
	if c.TTL() != 5*time.Second {
		t.Errorf("TTL = %v, mau 5s", c.TTL())
	}
}

var errBoom = &testErr{}

type testErr struct{}

func (e *testErr) Error() string { return "boom" }
