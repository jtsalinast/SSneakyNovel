package httpapi

import (
	"testing"
	"time"

	"showmethestory/internal/config"
	"showmethestory/internal/llm"
)

func TestLongTaskCeilingFloor(t *testing.T) {
	cases := []struct {
		minutes int
		want    time.Duration
	}{
		{0, minLongTaskCeiling},          // unset: generic 30m default is raised
		{30, minLongTaskCeiling},         // explicit but too small
		{60 * 24, 60 * 24 * time.Minute}, // larger configured value wins
		{-1, 0},                          // no ceiling stays unlimited
	}
	for _, c := range cases {
		h := &Handlers{apiCfg: &config.APIConfig{TaskDurationMinutes: c.minutes}}
		if got := h.longTaskCeiling(); got != c.want {
			t.Fatalf("task_duration_minutes=%d: want %v, got %v", c.minutes, c.want, got)
		}
	}
	h := &Handlers{apiCfg: &config.APIConfig{}}
	if got := h.taskCeiling(); got != llm.MaxTaskDuration {
		t.Fatalf("generic ceiling must stay at %v, got %v", llm.MaxTaskDuration, got)
	}
}
