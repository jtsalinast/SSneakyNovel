package prose

import "testing"

func TestCountProseUnits(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"Hello World", 2},
		{"3.14", 1},
		{"3,141", 1},
		{"U.S.A", 1},
		{"U.S.A.", 1},
		{"A#123", 1},
		{"0734-v4", 1},
		{"ＡＢＣ", 1},
		{"Price is 3.14", 3},
		{"He said. Hello", 3},
		{"", 0},
		{"   ", 0},
		{"。，！？", 0},
		{"a", 1},
		{"#123", 1},
		{".5", 1},
		// Each CJK ideograph counts as one word (Word/LibreOffice "字数" convention);
		// CJK punctuation is not counted.
		{"你好", 2},
		{"你好吗", 3},
		{"他说了一句话。", 6}, // 6 hanzi; the fullwidth period is not counted
		{"Hello 世界", 3},
	}
	for _, tt := range tests {
		got := CountProseUnits(tt.in)
		if got != tt.want {
			t.Errorf("CountProseUnits(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
