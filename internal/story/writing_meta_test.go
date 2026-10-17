package story

import (
"showmethestory/internal/i18n"
"testing"
)

func TestStripChapterMetaProse(t *testing.T) {
tests := []struct {
name string
in   string
want string
}{
{
name: "en basic",
in:   "Chapter 1: Arrival\n\nShe opened the door.\n\nEnd of chapter",
want: "She opened the door.",
},
{
name: "en preamble here is",
in:   "Here is the revised Chapter 3:\n\nThe door opened.\n\nEnd of chapter",
want: "The door opened.",
},
{
name: "en part heading with to be continued",
in:   "Part 2 Rising Wind\n\nThe wind crossed the hills.\n\nTo be continued",
want: "The wind crossed the hills.",
},
{
name: "en parenthesized chapter meta",
in:   "(Chapter 5 text)\n\nNight fell deep.\n\n(End)",
want: "Night fell deep.",
},
{
name: "en revision preamble line",
in:   "Revised full text of Chapter 3 as follows:\n\nShe walked into the room.",
want: "She walked into the room.",
},
}

for _, tt := range tests {
t.Run(tt.name, func(t *testing.T) {
got := stripChapterMetaProse(tt.in, i18n.LangEN)
if got != tt.want {
t.Fatalf("stripChapterMetaProse() = %q, want %q", got, tt.want)
}
})
}
}
