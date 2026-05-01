package dag

import (
	"strings"
	"testing"
	"time"
)

func TestWritePrometheusText(t *testing.T) {
	steps := []StepTiming{
		{ID: "warmup", Duration: 500 * time.Millisecond, ExitCode: 0},
		{ID: `step"x`, Duration: time.Second, ExitCode: 3},
	}
	var sb strings.Builder
	if err := WritePrometheusText(&sb, `pipe"y`, steps); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	if !strings.Contains(out, `pipeline="pipe\"y"`) || !strings.Contains(out, `step="step\"x"`) {
		t.Fatalf("label escaping:\n%s", out)
	}
	if !strings.Contains(out, "nexusflow_dag_step_duration_seconds") {
		t.Fatal(out)
	}
}
