package pythonsim_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/kenvaid/gos7"
)

type fixture struct {
	address, observer string
	t                 *testing.T
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	address := os.Getenv("S7_PYTHON_ADDRESS")
	if address == "" {
		t.Skip("set S7_PYTHON_ADDRESS and S7_PYTHON_OBSERVER for the local Python fixture")
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		t.Fatal("S7 address must be a literal loopback address")
	}
	observer := os.Getenv("S7_PYTHON_OBSERVER")
	u, err := url.Parse(observer)
	if err != nil || u.Scheme != "http" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() {
		t.Fatal("observer must use HTTP on a literal loopback address")
	}
	f := &fixture{address, observer, t}
	var health struct {
		Version, Mode string
		Areas         map[string]int
	}
	f.get("/health", &health)
	t.Logf("python-snap7=%s mode=%s address=%s areas=%v", health.Version, health.Mode, address, health.Areas)
	return f
}
func (f *fixture) get(path string, out any) {
	f.t.Helper()
	client := http.Client{Timeout: 3 * time.Second}
	r, err := client.Get(f.observer + path)
	if err != nil {
		f.t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		f.t.Fatal("observer status:", r.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024*1024)).Decode(out); err != nil {
		f.t.Fatal(err)
	}
}
func (f *fixture) memory() map[string][]byte { var m map[string][]byte; f.get("/memory", &m); return m }
func (f *fixture) connect(t *testing.T) gos7.ConnectedClient {
	t.Helper()
	h := gos7.NewTCPClientHandler(f.address, 0, 2)
	h.Timeout = 3 * time.Second
	h.IdleTimeout = 0
	if os.Getenv("S7_PYTHON_TRACE") == "1" {
		h.Logger = log.New(os.Stdout, "S7 ", 0)
	}
	c := gos7.NewClient(h).(gos7.ConnectedClient)
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func pattern(n, seed int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*37 + seed)
	}
	return b
}
func verifyMemory(t *testing.T, f *fixture, area string, want []byte) {
	t.Helper()
	got := f.memory()[area]
	if len(got) != len(want) {
		t.Fatalf("%s size got=%d expected=%d", area, len(got), len(want))
	}
	if !bytes.Equal(got, want) {
		for i := range got {
			if i >= len(want) || got[i] != want[i] {
				t.Fatalf("%s physical byte offset=%d got=%02x expected=%02x", area, i, got[i], want[i])
			}
		}
		t.Fatal("memory size mismatch")
	}
}

func TestPythonSnap7ByteAreas(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"M", "I", "Q"} {
		t.Run(name, func(t *testing.T) {
			c := f.connect(t)
			read, write := c.AGReadMB, c.AGWriteMB
			if name == "I" {
				read, write = c.AGReadEB, c.AGWriteEB
			}
			if name == "Q" {
				read, write = c.AGReadAB, c.AGWriteAB
			}
			expected := f.memory()[name]
			for _, n := range []int{1, 3, 16, 452, 462, 463, 480, 1024, 2048} {
				want := pattern(n, n)
				const start = 37
				if err := write(start, n, want); err != nil {
					t.Fatalf("length=%d write: %v", n, err)
				}
				copy(expected[start:], want)
				verifyMemory(t, f, name, expected)
				got := make([]byte, n)
				if err := read(start, n, got); err != nil || !bytes.Equal(got, want) {
					t.Fatalf("length=%d read: %v got=%x", n, err, got[:min(8, len(got))])
				}
			}
		})
	}
}

func TestPythonSnap7Bits(t *testing.T) {
	f := newFixture(t)
	for _, entry := range []struct {
		name string
		area int
	}{{"M", 0x83}, {"I", 0x81}, {"Q", 0x82}} {
		t.Run(entry.name, func(t *testing.T) {
			c := f.connect(t)
			expected := f.memory()[entry.name]
			for bit := 0; bit < 8; bit++ {
				want := byte((bit + 1) % 2)
				item := []gos7.S7DataItem{{Area: entry.area, WordLen: 1, Start: 17, Bit: bit, Amount: 1, Data: []byte{want}}}
				if err := c.AGWriteMulti(item, 1); err != nil || item[0].Error != "" {
					t.Fatal(err, item[0].Error)
				}
				expected[17] = (expected[17] &^ (1 << bit)) | (want << bit)
				verifyMemory(t, f, entry.name, expected)
				item[0].Data = []byte{0xcc}
				if err := c.AGReadMulti(item, 1); err != nil || item[0].Error != "" || item[0].Data[0] != want {
					t.Fatalf("bit=%d err=%v item=%+v", bit, err, item[0])
				}
			}
		})
	}
}

func TestPythonSnap7Counter(t *testing.T) {
	f := newFixture(t)
	for _, n := range []int{1, 3, 225, 226, 227, 300} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			c := f.connect(t)
			expected := f.memory()["CT"]
			const start = 7
			want := make([]byte, n*2)
			for i := 0; i < n; i++ {
				value := (i*31 + n) % 1000
				raw := uint16((value/100)<<8 | ((value/10)%10)<<4 | value%10)
				binary.BigEndian.PutUint16(want[i*2:], raw)
			}
			if err := c.AGWriteCT(start, n, want); err != nil {
				t.Fatal("write:", err)
			}
			copy(expected[start*2:], want)
			verifyMemory(t, f, "CT", expected)
			got := make([]byte, len(want))
			if err := c.AGReadCT(start, n, got); err != nil || !bytes.Equal(got, want) {
				t.Fatal("read:", err, "equal=", bytes.Equal(got, want))
			}
		})
	}
}

func TestPythonSnap7MixedCharReal(t *testing.T) {
	f := newFixture(t)
	for _, order := range [][]int{{0, 1, 2, 3, 4, 5, 6}, {6, 1, 0, 5, 4, 3, 2}, {1, 6, 5, 4, 0, 2, 3}} {
		t.Run(fmt.Sprint(order), func(t *testing.T) {
			c := f.connect(t)
			expected := f.memory()
			real := make([]byte, 12)
			for i, v := range []float32{1.25, -12.5, 12345.75} {
				binary.BigEndian.PutUint32(real[i*4:], math.Float32bits(v))
			}
			items := []gos7.S7DataItem{
				{Area: 0x83, WordLen: 2, Start: 512, Amount: 3, Data: []byte{1, 2, 3}},
				{Area: 0x81, WordLen: 3, Start: 512, Amount: 3, Data: []byte("S7!")},
				{Area: 0x82, WordLen: 4, Start: 512, Amount: 3, Data: pattern(6, 2)},
				{Area: 0x83, WordLen: 5, Start: 544, Amount: 3, Data: pattern(6, 3)},
				{Area: 0x81, WordLen: 6, Start: 544, Amount: 3, Data: pattern(12, 4)},
				{Area: 0x82, WordLen: 7, Start: 544, Amount: 3, Data: pattern(12, 5)},
				{Area: 0x83, WordLen: 8, Start: 576, Amount: 3, Data: real},
			}
			names := []string{"M", "I", "Q", "M", "I", "Q", "M"}
			ordered := make([]gos7.S7DataItem, len(items))
			for i, j := range order {
				ordered[i] = items[j]
			}
			if err := c.AGWriteMulti(ordered, len(ordered)); err != nil {
				t.Fatal("mixed write:", err)
			}
			for i, item := range ordered {
				if item.Error != "" {
					t.Fatalf("item %d: %s", i, item.Error)
				}
			}
			for i, item := range items {
				copy(expected[names[i]][item.Start:], item.Data)
			}
			for _, name := range []string{"M", "I", "Q"} {
				verifyMemory(t, f, name, expected[name])
			}
			for i := range ordered {
				ordered[i].Data = make([]byte, len(ordered[i].Data))
			}
			if err := c.AGReadMulti(ordered, len(ordered)); err != nil {
				t.Fatal("mixed read:", err)
			}
			for i, j := range order {
				if ordered[i].Error != "" || !bytes.Equal(ordered[i].Data, items[j].Data) {
					t.Fatalf("item %d: %+v", i, ordered[i])
				}
			}
		})
	}
}

func TestPythonSnap7MixedCounter(t *testing.T) {
	f := newFixture(t)
	c := f.connect(t)
	expected := f.memory()
	items := []gos7.S7DataItem{{Area: 0x83, WordLen: 2, Start: 900, Amount: 3, Data: []byte{1, 2, 3}}, {Area: 0x1c, WordLen: 0x1c, Start: 503, Amount: 3, Data: []byte{0x01, 0x23, 0x04, 0x56, 0x09, 0x99}}, {Area: 0x82, WordLen: 2, Start: 900, Amount: 1, Data: []byte{0x5a}}}
	if err := c.AGWriteMulti(items, len(items)); err != nil {
		t.Fatal(err)
	}
	for i, item := range items {
		if item.Error != "" {
			t.Fatal(i, item.Error)
		}
	}
	copy(expected["M"][900:], items[0].Data)
	copy(expected["CT"][1006:], items[1].Data)
	copy(expected["Q"][900:], items[2].Data)
	for _, name := range []string{"M", "CT", "Q"} {
		verifyMemory(t, f, name, expected[name])
	}
	wants := make([][]byte, len(items))
	for i := range items {
		wants[i] = bytes.Clone(items[i].Data)
		items[i].Data = make([]byte, len(items[i].Data))
	}
	if err := c.AGReadMulti(items, len(items)); err != nil {
		t.Fatal(err)
	}
	for i, item := range items {
		if item.Error != "" || !bytes.Equal(item.Data, wants[i]) {
			t.Fatal(i, item)
		}
	}
}

func TestPythonSnap7BoundsAndItemErrors(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"M", "I", "Q"} {
		t.Run(name, func(t *testing.T) {
			c := f.connect(t)
			read, write := c.AGReadMB, c.AGWriteMB
			if name == "I" {
				read, write = c.AGReadEB, c.AGWriteEB
			}
			if name == "Q" {
				read, write = c.AGReadAB, c.AGWriteAB
			}
			expected := f.memory()[name]
			if err := write(len(expected)-2, 3, []byte{0x11, 0x22, 0x33}); err == nil {
				t.Fatal("out-of-range write reported success")
			}
			verifyMemory(t, f, name, expected)
			buffer := []byte{0xcc}
			if err := read(len(expected), 1, buffer); err == nil || buffer[0] != 0xcc {
				t.Fatal("invalid read accepted or buffer modified:", err)
			}
			if err := read(0, 1, buffer); err != nil {
				t.Fatal("session lost after CPU refusal:", err)
			}
		})
	}
	t.Run("PartialMultiWrite", func(t *testing.T) {
		c := f.connect(t)
		expected := f.memory()
		items := []gos7.S7DataItem{{Area: 0x83, WordLen: 2, Start: 1100, Amount: 1, Data: []byte{0x99}}, {Area: 0x81, WordLen: 2, Start: len(expected["I"]) - 1, Amount: 3, Data: []byte{1, 2, 3}}, {Area: 0x82, WordLen: 2, Start: 1100, Amount: 3, Data: []byte{4, 5, 6}}}
		if err := c.AGWriteMulti(items, 3); err != nil {
			t.Fatal(err)
		}
		if items[0].Error != "" || items[1].Error == "" || items[2].Error != "" {
			t.Fatal(items)
		}
		copy(expected["M"][1100:], items[0].Data)
		copy(expected["Q"][1100:], items[2].Data)
		for _, name := range []string{"M", "I", "Q"} {
			verifyMemory(t, f, name, expected[name])
		}
		if err := c.AGReadMB(0, 1, []byte{0}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("PartialMultiRead", func(t *testing.T) {
		c := f.connect(t)
		expected := f.memory()
		items := []gos7.S7DataItem{{Area: 0x1c, WordLen: 0x1c, Start: len(expected["CT"]) / 2, Amount: 1, Data: []byte{0xcc, 0xcc}}, {Area: 0x83, WordLen: 2, Start: 1100, Amount: 3, Data: make([]byte, 3)}, {Area: 0x82, WordLen: 2, Start: 1100, Amount: 1, Data: make([]byte, 1)}}
		if err := c.AGReadMulti(items, 3); err != nil {
			t.Fatal(err)
		}
		if items[0].Error == "" || !bytes.Equal(items[0].Data, []byte{0xcc, 0xcc}) || items[1].Error != "" || items[2].Error != "" || !bytes.Equal(items[1].Data, expected["M"][1100:1103]) || items[2].Data[0] != expected["Q"][1100] {
			t.Fatal(items)
		}
		if err := c.AGReadMB(0, 1, []byte{0}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestPythonSnap7ConcurrentAreas(t *testing.T) {
	f := newFixture(t)
	for _, shared := range []bool{true, false} {
		t.Run(fmt.Sprintf("shared=%t", shared), func(t *testing.T) {
			const workers, iterations = 8, 50
			clients := make([]gos7.ConnectedClient, workers)
			clients[0] = f.connect(t)
			for i := 1; i < workers; i++ {
				clients[i] = clients[0]
				if !shared {
					clients[i] = f.connect(t)
				}
			}
			var wg sync.WaitGroup
			errs := make(chan error, workers)
			for i := 0; i < workers; i++ {
				wg.Add(1)
				go func(w int) {
					defer wg.Done()
					read, write := clients[w].AGReadMB, clients[w].AGWriteMB
					if w%3 == 1 {
						read, write = clients[w].AGReadEB, clients[w].AGWriteEB
					}
					if w%3 == 2 {
						read, write = clients[w].AGReadAB, clients[w].AGWriteAB
					}
					for k := 0; k < iterations; k++ {
						want := pattern(32, w*31+k)
						start := 2048 + w*64
						if err := write(start, len(want), want); err != nil {
							errs <- err
							return
						}
						got := make([]byte, len(want))
						if err := read(start, len(got), got); err != nil || !bytes.Equal(got, want) {
							errs <- fmt.Errorf("worker=%d iteration=%d err=%v mismatch=%t", w, k, err, !bytes.Equal(got, want))
							return
						}
					}
				}(i)
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Error(err)
			}
			t.Logf("workers=%d read/write requests=%d", workers, workers*iterations*2)
		})
	}
}
