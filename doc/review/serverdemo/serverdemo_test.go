package serverdemo_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/kenvaid/gos7"
)

// These integration tests are opt-in and restricted to a local simulator.
func connect(t *testing.T) (*gos7.TCPClientHandler, gos7.ConnectedClient) {
	t.Helper()
	address := os.Getenv("S7_SERVERDEMO_ADDRESS")
	if address == "" {
		t.Skip("set S7_SERVERDEMO_ADDRESS to run against the local simulator")
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		t.Fatal("only a literal loopback address is allowed")
	}
	h := gos7.NewTCPClientHandler(address, 0, 2)
	h.Timeout, h.IdleTimeout = 3*time.Second, 0
	if os.Getenv("S7_SERVERDEMO_TRACE") == "1" {
		h.Logger = log.New(os.Stdout, "S7 ", 0)
	}
	c := gos7.NewClient(h).(gos7.ConnectedClient)
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return h, c
}

func TestServerDemoReadWrite(t *testing.T) {
	_, c := connect(t)
	originals := map[int][]byte{}
	for db := 1; db <= 3; db++ {
		bi, err := c.GetAgBlockInfo(0x41, db)
		if err != nil {
			t.Fatal(err)
		}
		if bi.MC7Size < 1024 || bi.MC7Size > 65535 {
			t.Fatalf("DB%d size unsuitable: %d", db, bi.MC7Size)
		}
		b := make([]byte, bi.MC7Size)
		if err := c.AGReadDB(db, 0, len(b), b); err != nil {
			t.Fatal(err)
		}
		originals[db] = b
	}
	// Save recovery data before the first write, including after a killed test run.
	if path := os.Getenv("S7_SERVERDEMO_BACKUP"); path != "" {
		b, err := json.MarshalIndent(originals, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
		t.Logf("original DB data saved to %s", path)
	}
	t.Cleanup(func() {
		_, restore := connect(t)
		for db := 1; db <= 3; db++ {
			b := originals[db]
			if err := restore.AGWriteDB(db, 0, len(b), b); err != nil {
				t.Errorf("RESTORE DB%d: %v", db, err)
				continue
			}
			got := make([]byte, len(b))
			if err := restore.AGReadDB(db, 0, len(got), got); err != nil || !bytes.Equal(got, b) {
				t.Errorf("RESTORE verification DB%d: %v", db, err)
			}
		}
		t.Log("original DB1/DB2/DB3 data restored and verified")
	})
	run := func(name string, body func(*testing.T)) {
		t.Run(name, func(t *testing.T) {
			if err := c.Connect(); err != nil {
				t.Fatal(err)
			}
			body(t)
		})
	}
	run("DBChunking", func(t *testing.T) {
		for db := 1; db <= 3; db++ {
			for _, n := range []int{1, 3, 16, 452, 462, 463, 480, 1024, len(originals[db])} {
				want := make([]byte, n)
				for i := range want {
					want[i] = byte(i*37 + n + db)
				}
				if err := c.AGWriteDB(db, 0, n, want); err != nil {
					t.Fatalf("DB%d length=%d write: %v", db, n, err)
				}
				got := make([]byte, n)
				if err := c.AGReadDB(db, 0, n, got); err != nil || !bytes.Equal(got, want) {
					t.Fatalf("DB%d length=%d read: %v", db, n, err)
				}
			}
		}
	})
	run("Bits", func(t *testing.T) {
		for bit := 0; bit < 8; bit++ {
			if err := c.AGWriteDB(1, 17, 1, []byte{0xaa}); err != nil {
				t.Fatal(err)
			}
			want := byte((bit + 1) % 2)
			items := []gos7.S7DataItem{{Area: 0x84, WordLen: 1, DBNumber: 1, Start: 17, Bit: bit, Amount: 1, Data: []byte{want}}}
			if err := c.AGWriteMulti(items, 1); err != nil || items[0].Error != "" {
				t.Fatal(err, items[0].Error)
			}
			got := []byte{0}
			if err := c.AGReadDB(1, 17, 1, got); err != nil {
				t.Fatal(err)
			}
			expect := byte(0xaa &^ (1 << bit))
			expect |= want << bit
			if got[0] != expect {
				t.Fatalf("bit=%d got=%02x want=%02x", bit, got[0], expect)
			}
			items[0].Data = []byte{0xff}
			if err := c.AGReadMulti(items, 1); err != nil || items[0].Error != "" || items[0].Data[0] != want {
				t.Fatal(err, items)
			}
		}
	})
	run("MultiTypesAndOddPadding", func(t *testing.T) {
		widths := []int{1, 1, 2, 2, 4, 4, 4}
		items := make([]gos7.S7DataItem, 7)
		for i, word := range []int{2, 3, 4, 5, 6, 7, 8} {
			b := make([]byte, widths[i]*3)
			for j := range b {
				b[j] = byte(i*19 + j + 1)
			}
			items[i] = gos7.S7DataItem{Area: 0x84, WordLen: word, DBNumber: 1 + i%3, Start: 64 + i*32, Amount: 3, Data: b}
		}
		// The native server locates subsequent write items using the variable
		// type rather than the data transport type. CHAR/REAL are written alone
		// here, then all types are read together. The limitation is reported.
		for _, i := range []int{1, 6} {
			one := items[i : i+1]
			if err := c.AGWriteMulti(one, 1); err != nil || one[0].Error != "" {
				t.Fatal(err, one[0].Error)
			}
		}
		regular := []gos7.S7DataItem{items[0], items[2], items[3], items[4], items[5]}
		if err := c.AGWriteMulti(regular, len(regular)); err != nil {
			t.Fatal(err)
		}
		for _, item := range regular {
			if item.Error != "" {
				t.Fatal(item.Error)
			}
		}
		wants := make([][]byte, len(items))
		for i := range items {
			if items[i].Error != "" {
				t.Fatal(items[i].Error)
			}
			wants[i] = bytes.Clone(items[i].Data)
			items[i].Data = make([]byte, len(items[i].Data))
		}
		if err := c.AGReadMulti(items, len(items)); err != nil {
			t.Fatal(err)
		}
		for i := range items {
			if items[i].Error != "" || !bytes.Equal(items[i].Data, wants[i]) {
				t.Fatalf("item %d: %s got=%x want=%x", i, items[i].Error, items[i].Data, wants[i])
			}
		}
	})
	run("Multi20Items", func(t *testing.T) {
		items := make([]gos7.S7DataItem, 20)
		for i := range items {
			items[i] = gos7.S7DataItem{Area: 0x84, WordLen: 2, DBNumber: 2, Start: 512 + i*4, Amount: 3, Data: []byte{byte(i), 0x55, 0xaa}}
		}
		if err := c.AGWriteMulti(items, 20); err != nil {
			t.Fatal(err)
		}
		for i := range items {
			if items[i].Error != "" {
				t.Fatal(items[i].Error)
			}
			items[i].Data = make([]byte, 3)
		}
		if err := c.AGReadMulti(items, 20); err != nil {
			t.Fatal(err)
		}
		for i := range items {
			if items[i].Error != "" || !bytes.Equal(items[i].Data, []byte{byte(i), 0x55, 0xaa}) {
				t.Fatal(i, items[i])
			}
		}
	})
	run("DBFillAndDBGet", func(t *testing.T) {
		if err := c.DBFill(3, 0x5a); err != nil {
			t.Fatal(err)
		}
		b := make([]byte, len(originals[3]))
		if err := c.DBGet(3, b, len(b)); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(b, bytes.Repeat([]byte{0x5a}, len(b))) {
			t.Fatal("DBGet/DBFill mismatch")
		}
	})
	run("AddressSyntax", func(t *testing.T) {
		if err := c.AGWriteDB(1, 0, 4, []byte{0x12, 0x34, 0x56, 0x78}); err != nil {
			t.Fatal(err)
		}
		v, err := c.Read("DB1.DBW0", make([]byte, 2))
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("DB1.DBW0=%v", v)
	})
	run("ErrorsPreserveSession", func(t *testing.T) {
		if err := c.AGReadDB(65535, 0, 1, []byte{0}); err == nil {
			t.Fatal("missing DB accepted")
		}
		if err := c.AGReadDB(1, len(originals[1]), 1, []byte{0}); err == nil {
			t.Fatal("out-of-range read accepted")
		}
		items := []gos7.S7DataItem{{Area: 0x84, WordLen: 2, DBNumber: 1, Amount: 1, Data: []byte{0}}, {Area: 0x84, WordLen: 2, DBNumber: 65535, Amount: 1, Data: []byte{0xcc}}}
		if err := c.AGReadMulti(items, 2); err != nil {
			t.Fatal(err)
		}
		if items[0].Error != "" || items[1].Error == "" || items[1].Data[0] != 0xcc {
			t.Fatal(items)
		}
		if err := c.AGReadDB(1, 0, 1, []byte{0}); err != nil {
			t.Fatal("session lost after legal CPU refusal:", err)
		}
	})
	for _, independent := range []bool{false, true} {
		name := "SharedConnection"
		if independent {
			name = "IndependentConnections"
		}
		run(name, func(t *testing.T) {
			const workers, iterations = 8, 100
			clients := make([]gos7.ConnectedClient, workers)
			for i := range clients {
				clients[i] = c
				if independent {
					_, clients[i] = connect(t)
				}
			}
			var wg sync.WaitGroup
			errs := make(chan error, workers)
			start := make(chan struct{})
			for worker := 0; worker < workers; worker++ {
				wg.Add(1)
				go func(w int) {
					defer wg.Done()
					<-start
					for iteration := 0; iteration < iterations; iteration++ {
						want := make([]byte, 32)
						for j := range want {
							want[j] = byte(w*31 + iteration + j)
						}
						if err := clients[w].AGWriteDB(1, 1024+w*64, len(want), want); err != nil {
							errs <- fmt.Errorf("worker %d write %d: %w", w, iteration, err)
							return
						}
						got := make([]byte, len(want))
						if err := clients[w].AGReadDB(1, 1024+w*64, len(got), got); err != nil || !bytes.Equal(got, want) {
							errs <- fmt.Errorf("worker %d read %d: err=%v mismatch=%t", w, iteration, err, !bytes.Equal(got, want))
							return
						}
					}
				}(worker)
			}
			began := time.Now()
			close(start)
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Error(err)
			}
			elapsed := time.Since(began)
			if elapsed <= 0 {
				elapsed = time.Nanosecond
			}
			t.Logf("workers=%d iterations=%d requests=%d elapsed=%s requests/s=%.0f", workers, iterations, workers*iterations*2, elapsed, float64(workers*iterations*2)/elapsed.Seconds())
		})
	}
}

func TestServerDemoOtherAreas(t *testing.T) {
	_, c := connect(t)
	for _, area := range []struct {
		name          string
		read, write   func(int, int, []byte) error
		amount, width int
	}{
		{"M", c.AGReadMB, c.AGWriteMB, 16, 1}, {"I", c.AGReadEB, c.AGWriteEB, 16, 1}, {"Q", c.AGReadAB, c.AGWriteAB, 16, 1}, {"CT", c.AGReadCT, c.AGWriteCT, 8, 2}, {"TM", c.AGReadTM, c.AGWriteTM, 8, 2},
	} {
		t.Run(area.name, func(t *testing.T) {
			if err := c.Connect(); err != nil {
				t.Fatal(err)
			}
			b := make([]byte, area.amount*area.width)
			if err := area.read(0, area.amount, b); err != nil {
				if err.Error() != "s7: item error 0a: CPU : Item not available" {
					t.Fatal(err)
				}
				t.Skipf("area unavailable in this ServerDemo configuration: %v", err)
			}
			t.Cleanup(func() {
				if err := area.write(0, area.amount, b); err != nil {
					t.Errorf("restore %s: %v", area.name, err)
				}
				got := make([]byte, len(b))
				if err := area.read(0, area.amount, got); err != nil || !bytes.Equal(got, b) {
					t.Errorf("restore verification %s: %v", area.name, err)
				}
			})
			want := make([]byte, len(b))
			for i := range want {
				want[i] = byte(i*13 + 7)
			}
			if err := area.write(0, area.amount, want); err != nil {
				t.Fatal(err)
			}
			got := make([]byte, len(b))
			if err := area.read(0, area.amount, got); err != nil || !bytes.Equal(got, want) {
				t.Fatal(err, "readback mismatch")
			}
		})
	}
}

func TestServerDemoReconnect(t *testing.T) {
	_, c := connect(t)
	for i := 0; i < 5; i++ {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		if err := c.Connect(); err != nil {
			t.Fatal(err)
		}
		if err := c.AGReadDB(1, 0, 1, []byte{0}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestServerDemoManagementWrites(t *testing.T) {
	_, c := connect(t)
	t.Run("SessionPassword", func(t *testing.T) {
		if err := c.SetSessionPassword("s7test"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := c.Connect(); err != nil {
				t.Error(err)
				return
			}
			if err := c.ClearSessionPassword(); err != nil {
				t.Error(err)
			}
		})
		if err := c.ClearSessionPassword(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("SetClockAcknowledgement", func(t *testing.T) {
		if err := c.Connect(); err != nil {
			t.Fatal(err)
		}
		now, err := c.GetPLCDateTime()
		if err != nil {
			t.Fatal(err)
		}
		// Native Snap7 Server acknowledges but does not change its host clock.
		if err := c.SetPLCDateTime(now); err != nil {
			t.Fatal(err)
		}
		if _, err := c.GetPLCDateTime(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("StopHotColdStart", func(t *testing.T) {
		if err := c.Connect(); err != nil {
			t.Fatal(err)
		}
		original, err := c.PLCGetStatus()
		if err != nil {
			t.Fatal(err)
		}
		if original != 4 && original != 8 {
			t.Fatal("cannot restore unknown CPU status")
		}
		t.Cleanup(func() {
			_, r := connect(t)
			got, e := r.PLCGetStatus()
			if e != nil {
				t.Error(e)
				return
			}
			if got != original {
				if original == 4 {
					e = r.PLCStop()
				} else {
					e = r.PLCHotStart()
				}
				if e != nil {
					t.Error("restore CPU status:", e)
				}
			}
			got, e = r.PLCGetStatus()
			if e != nil || got != original {
				t.Error("verify original CPU status:", got, e)
			}
		})
		for _, step := range []struct {
			name string
			call func() error
			want int
		}{{"Stop", c.PLCStop, 4}, {"HotStart", c.PLCHotStart, 8}, {"StopAgain", c.PLCStop, 4}, {"ColdStart", c.PLCColdStart, 8}} {
			if err := step.call(); err != nil {
				t.Fatal(step.name, err)
			}
			got, err := c.PLCGetStatus()
			if err != nil || got != step.want {
				t.Fatal(step.name, got, err)
			}
		}
	})
}

func TestServerDemoDiscovery(t *testing.T) {
	h, c := connect(t)
	pdu, err := h.PDUSize()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("address=%s rack=0 slot=2 negotiated PDU=%d", h.Address, pdu)
	checks := []struct {
		name string
		run  func(*testing.T)
	}{
		{"OrderCode", func(t *testing.T) {
			v, e := c.GetOrderCode()
			if e != nil {
				t.Fatal(e)
			}
			t.Logf("%+v", v)
		}},
		{"CPUInfo", func(t *testing.T) {
			v, e := c.GetCPUInfo()
			if e != nil {
				t.Fatal(e)
			}
			t.Logf("%+v", v)
		}},
		{"CPInfo", func(t *testing.T) {
			v, e := c.GetCPInfo()
			if e != nil {
				t.Fatal(e)
			}
			t.Logf("%+v", v)
		}},
		{"Protection", func(t *testing.T) {
			v, e := c.GetProtection()
			if e != nil {
				t.Fatal(e)
			}
			t.Logf("%+v", v)
		}},
		{"CPUStatus", func(t *testing.T) {
			v, e := c.PLCGetStatus()
			if e != nil {
				t.Fatal(e)
			}
			t.Logf("status=%d", v)
		}},
		{"Clock", func(t *testing.T) {
			v, e := c.GetPLCDateTime()
			if e != nil {
				t.Fatal(e)
			}
			t.Logf("%s", v.Format(time.RFC3339Nano))
		}},
		{"Directory", func(t *testing.T) {
			v, e := c.PGListBlocks()
			if e != nil {
				t.Fatal(e)
			}
			t.Logf("%+v", v)
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if e := c.Connect(); e != nil {
				t.Fatal(e)
			}
			check.run(t)
		})
	}
	for db := 1; db <= 3; db++ {
		t.Run(string(rune('0'+db)), func(t *testing.T) {
			if e := c.Connect(); e != nil {
				t.Fatal(e)
			}
			v, e := c.GetAgBlockInfo(0x41, db)
			if e != nil {
				t.Fatal(e)
			}
			t.Logf("DB%d: %+v", db, v)
			if v.MC7Size <= 0 || v.MC7Size > 65535 {
				t.Fatal("invalid DB size")
			}
			buf := make([]byte, v.MC7Size)
			if e := c.AGReadDB(db, 0, len(buf), buf); e != nil {
				t.Fatal(e)
			}
			t.Logf("read %d bytes", len(buf))
		})
	}
}
