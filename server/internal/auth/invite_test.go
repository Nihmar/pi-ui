package auth

import (
	"math/rand"
	"strings"
	"testing"
)

// The normalization table is the contract both the server and the app mirror. The
// `o1l-m27x` case follows from the rules literally (O→0, `1` kept, L→1), which is why
// it folds to `011M27X`; the two normalizers produce this same value, and the app's
// `O I L 0 → 0110` vector depends on the same rule.
func TestNormalizeCode(t *testing.T) {
	for input, want := range map[string]string{
		"o1l-m27x":    "011M27X",
		" 4k9 m27 ":   "4K9M27",
		"4k9m27":      "4K9M27",
		"4K9_M27":     "4K9M27",
		"4K9-M27":     "4K9M27",
		"  O I L 0  ": "0110",
		"":            "",
		"   ":         "",
	} {
		if got := NormalizeCode(input); got != want {
			t.Errorf("NormalizeCode(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNewInviteCodeUsesTheWholeAlphabetUniformly(t *testing.T) {
	random := rand.New(rand.NewSource(1))
	const codes = 1024
	counts := make(map[byte]int, len(inviteAlphabet))
	for i := 0; i < codes; i++ {
		code, err := newInviteCode(random)
		if err != nil {
			t.Fatalf("newInviteCode: %v", err)
		}
		if len(code) != inviteCodeLength {
			t.Fatalf("code %q has length %d, want %d", code, len(code), inviteCodeLength)
		}
		for j := 0; j < len(code); j++ {
			if !strings.ContainsRune(inviteAlphabet, rune(code[j])) {
				t.Fatalf("code %q uses a symbol outside the alphabet", code)
			}
			counts[code[j]]++
		}
	}

	// Every symbol must be reachable (a modulo bias or a truncated alphabet would
	// leave most of them out) and roughly as likely as any other. The bounds are wide
	// because this is a smoke test for uniformity, not a chi-square.
	draws := float64(codes * inviteCodeLength)
	expected := draws / float64(len(inviteAlphabet))
	for i := 0; i < len(inviteAlphabet); i++ {
		symbol := inviteAlphabet[i]
		count := float64(counts[symbol])
		if count == 0 {
			t.Fatalf("symbol %q never appeared across %d codes", string(symbol), codes)
		}
		if count < expected*0.5 || count > expected*1.5 {
			t.Errorf("symbol %q appeared %.0f times, want about %.0f", string(symbol), count, expected)
		}
	}
}

func TestConstantTimeCodeEqualPadsShortGuesses(t *testing.T) {
	if !constantTimeCodeEqual("4K9M27", "4K9M27") {
		t.Fatal("an identical code did not match")
	}
	if constantTimeCodeEqual("4K9M27", "4K9M2") {
		t.Fatal("a truncated code matched")
	}
	if constantTimeCodeEqual("4K9M27", "4K9M28") {
		t.Fatal("a different code matched")
	}
}
