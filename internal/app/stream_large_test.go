package app

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSavedVariablesWithBrokenTailIsNotCommitted(t *testing.T) {
	b := nouveauBancStats(t)
	path := b.fichier("ForeverPulse.lua", append(fixture(1, 3, 1), []byte("\nForeverPulseScanDB={ broken() }\n")...))
	if _, e := b.a.ProcessFile(context.Background(), path); e == nil {
		t.Fatal("code accepted as data")
	}
	if b.st.Counts()["pending"] != 0 {
		t.Fatal("prefix committed from a broken file")
	}
	valid := b.fichier("ForeverPulse.lua.bak", fixture(1, 3, 1))
	if added, e := b.a.ProcessFile(context.Background(), valid); e != nil || added != 1 {
		t.Fatalf("backup: %d %v", added, e)
	}
}

func TestLargeSavedVariables(t *testing.T) {
	if os.Getenv("FP_TEST_LARGE_SAVED_VARIABLES") != "1" {
		t.Skip("set FP_TEST_LARGE_SAVED_VARIABLES=1 for large local synthetic fixtures")
	}
	for _, mib := range []int{80, 384} {
		t.Run(fmt.Sprint(mib), func(t *testing.T) {
			b := nouveauBancStats(t)
			path := filepath.Join(b.dir, "ForeverPulse.lua")
			f, e := os.Create(path)
			if e != nil {
				t.Fatal(e)
			}
			w := bufio.NewWriterSize(f, 128<<10)
			_, _ = w.Write(fixture(1, 3, 1))
			// Part of the bytes are in processed Stats records; part are
			// in a private addon index, scanned for syntax and skipped.
			_, _ = fmt.Fprint(w, "ForeverPulseStatsDB={schema='forever-pulse-stats',version=4,ctx={'beta-4619-PVP'},contextes={['beta-4619-PVP']={environment='beta',realm_id=4619,ruleset='PVP',region='90'}},personnages={")
			block := strings.Repeat("x", 128<<10)
			for i := 0; i < mib*2; i++ {
				_, _ = fmt.Fprintf(w, "['Player-4619-%08X']={n='Synthetic',c='MAGE',ra='Human',f='A',s=2,l=20,x=1,d=1730000000,o=1730000000,v={'1730000000|20|n|60:9'},padding='", i+1)
				_, _ = w.WriteString(block)
				_, _ = w.WriteString("'},\n")
			}
			_, _ = w.WriteString("}}\nForeverPulseScanDB={")
			for range mib * 6 {
				_, _ = w.WriteString("'")
				_, _ = w.WriteString(block)
				_, _ = w.WriteString("',\n")
			}
			_, _ = w.WriteString("}\n")
			if e = w.Flush(); e != nil {
				t.Fatal(e)
			}
			if e = f.Close(); e != nil {
				t.Fatal(e)
			}
			info, e := os.Stat(path)
			if e != nil {
				t.Fatal(e)
			}
			runtime.GC()
			var wg sync.WaitGroup
			stop := make(chan struct{})
			peak := uint64(0)
			wg.Add(1)
			go func() {
				defer wg.Done()
				timer := time.NewTicker(10 * time.Millisecond)
				defer timer.Stop()
				for {
					select {
					case <-stop:
						return
					case <-timer.C:
						var s runtime.MemStats
						runtime.ReadMemStats(&s)
						peak = max(peak, s.HeapAlloc)
					}
				}
			}()
			start := time.Now()
			added, e := b.a.ProcessFile(context.Background(), path)
			elapsed := time.Since(start)
			close(stop)
			wg.Wait()
			t.Logf("bytes=%d elapsed=%s sampled_peak_heap=%d batches=%d stats=%d", info.Size(), elapsed, peak, added, b.st.StatsCounts()["pending"])
			if e != nil || added != 1 || b.st.StatsCounts()["pending"] != mib*2 {
				t.Fatalf("file not imported: %v", e)
			}
			if peak > 100<<20 {
				t.Fatalf("heap exceeded 100 MiB: %d", peak)
			}
		})
	}
}
