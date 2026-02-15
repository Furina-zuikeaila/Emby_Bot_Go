package router

import "testing"

func TestSafeInlineCode(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if got := safeInlineCode("   "); got != "" {
			t.Fatalf("expected empty, got %q", got)
		}
	})

	t.Run("replacesBackticksAndWhitespace", func(t *testing.T) {
		in := "  a`b \n c\r\td  "
		got := safeInlineCode(in)
		if got == "" {
			t.Fatalf("expected non-empty")
		}
		if got != "a'b   c  d" {
			t.Fatalf("unexpected: %q", got)
		}
	})

	t.Run("truncates", func(t *testing.T) {
		in := ""
		for i := 0; i < 200; i++ {
			in += "a"
		}
		got := safeInlineCode(in)
		if len(got) != 128 {
			t.Fatalf("expected len 128, got %d", len(got))
		}
	})
}
