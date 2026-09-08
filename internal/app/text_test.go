package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestUnicodeTextValidation(t *testing.T) {
	for _, text := range []string{"", "Café — Ελληνικά 中文 العربية हिन्दी 🚀 e\u0301 👩\u200d💻\n\t", "\U0010ffff"} {
		if err := validateText(text); err != nil {
			t.Fatal(err)
		}
	}
	for _, text := range []string{string([]byte{0xff}), "bad\x00", "bad\x1b", "\r\n", "\u0085", "\u007f"} {
		if validateText(text) == nil {
			t.Fatalf("accepted %q", text)
		}
	}
}

func TestBulkTextChunkingAndProgress(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "partial"}[fail], func(t *testing.T) {
			var chunks atomic.Int64
			b, calls := transactionFixture(t, func(q map[string]any) map[string]any {
				r := safeStatus()
				if q["op"] == "text_transaction" {
					n := chunks.Add(1)
					scalars := q["scalars"].([]any)
					for _, scalar := range scalars {
						if scalar != float64(0x754c) {
							return map[string]any{"ok": false, "error": "wrong_unicode_scalar"}
						}
					}
					if len(scalars) > textChunkRunes {
						return map[string]any{"ok": false, "error": "oversized"}
					}
					if fail && n == 2 {
						return map[string]any{"ok": false, "error": "restore failed", "completed_characters": 3}
					}
					r["completed_characters"] = len(scalars)
				}
				return r
			})
			// Drain requests to avoid filling the fixture's deliberately bounded log.
			done := make(chan struct{})
			go func() {
				for {
					select {
					case <-calls:
					case <-done:
						return
					}
				}
			}()
			defer close(done)
			text := strings.Repeat("界", 4097) // exceeds the old 4096-byte action limit
			_, err := b.input(context.Background(), "a", InputArgs{Window: "abc", Revision: "20,40,600,800", Actions: []Action{{Type: "text", Text: text}}})
			if fail {
				var partial *InputFailure
				if !errors.As(err, &partial) || partial.CompletedCharacters != textChunkRunes+3 || chunks.Load() != 2 {
					t.Fatal(err, chunks.Load())
				}
			} else if err != nil || chunks.Load() != int64((4097+textChunkRunes-1)/textChunkRunes) {
				t.Fatal(err, chunks.Load())
			}
		})
	}
}

func TestTextValidationBeforeAnyEffects(t *testing.T) {
	for _, text := range []string{"\x00", strings.Repeat("x", maxBatchTextBytes+1)} {
		b, calls := inputTransactionFixture(t, "")
		_, err := b.input(context.Background(), "a", InputArgs{Window: "abc", Revision: "20,40,600,800", Actions: []Action{{Type: "key", Key: "ENTER"}, {Type: "text", Text: text}}})
		if err == nil || len(calls) != 0 {
			t.Fatal("text validation followed input")
		}
	}
	b, calls := inputTransactionFixture(t, "")
	_, err := b.input(context.Background(), "a", InputArgs{Window: "abc", Revision: "20,40,600,800", Actions: []Action{{Type: "text", Text: strings.Repeat("x", maxBatchTextBytes)}, {Type: "text", Text: "x"}}})
	if err == nil || len(calls) != 0 {
		t.Fatal("missing aggregate batch budget")
	}
}

func TestTextRevocationBetweenChunks(t *testing.T) {
	var broker atomic.Pointer[Broker]
	b, calls := transactionFixture(t, func(q map[string]any) map[string]any {
		r := safeStatus()
		if q["op"] == "text_transaction" {
			r["completed_characters"] = len(q["scalars"].([]any))
			b := broker.Load()
			b.mu.Lock()
			b.paused = true
			b.mu.Unlock()
		}
		return r
	})
	broker.Store(b)
	_, err := b.input(context.Background(), "a", InputArgs{Window: "abc", Revision: "20,40,600,800", Actions: []Action{{Type: "text", Text: strings.Repeat("界", textChunkRunes+1)}}})
	var partial *InputFailure
	if !errors.As(err, &partial) || partial.CompletedCharacters != textChunkRunes {
		t.Fatal("revocation progress", err)
	}
	chunks := 0
	for len(calls) > 0 {
		if (<-calls)["op"] == "text_transaction" {
			chunks++
		}
	}
	if chunks != 1 {
		t.Fatal("text sent after revocation", chunks)
	}
}

func TestTextRequiresGuardCapabilityBeforeEarlierActions(t *testing.T) {
	b, calls := transactionFixture(t, func(map[string]any) map[string]any { r := safeStatus(); delete(r, "unicode_text"); return r })
	_, err := b.input(context.Background(), "a", InputArgs{Window: "abc", Revision: "20,40,600,800", Actions: []Action{{Type: "key", Key: "ENTER"}, {Type: "text", Text: "界"}}})
	if err == nil || !strings.Contains(err.Error(), "setup") {
		t.Fatal(err)
	}
	for len(calls) > 0 {
		if (<-calls)["op"] != "status" {
			t.Fatal("effect before feature negotiation")
		}
	}
}

func TestTextKeymapXKB(t *testing.T) {
	if os.Getenv("COMPUTER_USE_TEST_XKB") != "1" {
		t.Skip("set COMPUTER_USE_TEST_XKB=1 for real Unicode keymap validation")
	}
	text := []rune("é—Ελ中한عرह🚀e\u0301👩\u200d💻\ufe0f\n\t")
	text = append(text, 0x100, 0x10ffff)
	for len(text) < textChunkRunes {
		text = append(text, rune(0x400+len(text)))
	}
	if len(text) > textChunkRunes {
		t.Fatal("test chunk too long")
	}
	var cases [][3]uint32
	for i, r := range text {
		cases = append(cases, [3]uint32{uint32([]int{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 43, 44, 45, 46, 47, 48, 49, 50, 51, 52, 53, 57}[i] + 8), textKeysym(r), uint32(r)})
	}
	// Compile and execute the actual native generator, not a Go facsimile.
	binary := filepath.Join(t.TempDir(), "native-text-test")
	if out, err := exec.Command("c++", "-std=c++23", "../../native/input_transaction_test.cpp", "-o", binary).CombinedOutput(); err != nil {
		t.Fatalf("native test build: %v\n%s", err, out)
	}
	args := []string{"--text-keymap"}
	for _, r := range text {
		args = append(args, strconv.Itoa(int(r)))
	}
	keymap, err := exec.Command(binary, args...).Output()
	if err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(map[string]any{"keymap": string(keymap), "cases": cases})
	script := `import ctypes as c,json,sys
x=c.CDLL('libxkbcommon.so.0')
def fn(n,r,*a):
 f=getattr(x,n); f.restype=r; f.argtypes=list(a); return f
p=c.c_void_p; u=c.c_uint32
data=json.load(sys.stdin)
ctx=fn('xkb_context_new',p,c.c_int)(0)
k=fn('xkb_keymap_new_from_string',p,p,c.c_char_p,c.c_int,c.c_int)(ctx,data['keymap'].encode(),1,0)
assert k
s=fn('xkb_state_new',p,p)(k)
for code,sym,scalar in data['cases']:
 got=fn('xkb_state_key_get_one_sym',u,p,u)(s,code)
 assert got==sym,(code,hex(got),hex(sym))
 if scalar not in (9,10):
  got=fn('xkb_state_key_get_utf32',u,p,u)(s,code)
  assert got==scalar,(code,hex(got),hex(scalar))
fn('xkb_state_unref',None,p)(s)
fn('xkb_keymap_unref',None,p)(k)
fn('xkb_context_unref',None,p)(ctx)
`
	cmd := exec.Command("python3", "-c", script)
	cmd.Stdin = bytes.NewReader(input)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func textKeysym(r rune) uint32 {
	switch r {
	case '\n':
		return 0xff0d
	case '\t':
		return 0xff09
	}
	if r <= 0xff {
		return uint32(r)
	}
	return 0x01000000 | uint32(r)
}

func TestTextMalformedProgressKeepsOnlyAcknowledgedChunks(t *testing.T) {
	for _, count := range []any{nil, -1, 0, 3, 1.5} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			var chunks atomic.Int64
			b, _ := transactionFixture(t, func(q map[string]any) map[string]any {
				r := safeStatus()
				if q["op"] == "text_transaction" {
					if chunks.Add(1) == 1 {
						r["completed_characters"] = textChunkRunes
					} else if count != nil {
						r["completed_characters"] = count
					}
				}
				return r
			})
			// The final chunk contains two scalars; every reply above is invalid.
			_, err := b.input(context.Background(), "a", InputArgs{Window: "abc", Revision: "20,40,600,800", Actions: []Action{{Type: "text", Text: strings.Repeat("x", textChunkRunes+2)}}})
			var partial *InputFailure
			if !errors.As(err, &partial) || partial.CompletedCharacters != textChunkRunes || chunks.Load() != 2 {
				t.Fatal(err, chunks.Load())
			}
		})
	}
}

func TestTextCancellationDuringChunkPreservesPriorProgress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release := make(chan struct{})
	defer close(release)
	var chunks atomic.Int64
	b, _ := transactionFixture(t, func(q map[string]any) map[string]any {
		r := safeStatus()
		if q["op"] == "text_transaction" {
			if chunks.Add(1) == 2 {
				cancel()
				<-release // simulate a lost reply from an already-admitted callback
			}
			r["completed_characters"] = len(q["scalars"].([]any))
		}
		return r
	})
	// Use an existing grant so the test does not wait for YOLO token cleanup
	// behind the deliberately stalled mock callback.
	b.mode = "approve"
	b.grants["text-test"] = &Grant{ID: "text-test", Client: "a", Capability: "control", Scope: Scope{"window", "abc"}, Expires: b.now().Add(time.Minute)}
	_, err := b.input(ctx, "a", InputArgs{Window: "abc", Revision: "20,40,600,800", Actions: []Action{{Type: "text", Text: strings.Repeat("x", textChunkRunes*3)}}})
	var partial *InputFailure
	if !errors.As(err, &partial) || partial.CompletedCharacters != textChunkRunes || chunks.Load() != 2 {
		t.Fatal(err, chunks.Load())
	}
}
