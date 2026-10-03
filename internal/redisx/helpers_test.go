package redisx

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

func itoa(n int) string { return strconv.Itoa(n) }
func atoi(s string) int { n, _ := strconv.Atoi(s); return n }
func sscan(line string, n *int) (int, error) {
	return fmt.Sscanf(strings.TrimSpace(line), "*%d", n)
}
func sscan2(line string, n *int)                        { fmt.Sscanf(strings.TrimSpace(line), "$%d", n) }
func readFull(r *bufio.Reader, buf []byte) (int, error) { return ioReadFull(r, buf) }
