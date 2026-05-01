package dag

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// StepTiming records one executed pipeline vertex for Prometheus text export.
type StepTiming struct {
	ID       string
	Duration time.Duration
	ExitCode int
}

func promEscLabel(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// WritePrometheusText writes HELP/TYPE lines and gauges for DAG steps (Prometheus exposition format).
func WritePrometheusText(w io.Writer, pipeline string, steps []StepTiming) error {
	pl := promEscLabel(pipeline)
	var sb strings.Builder
	sb.WriteString("# HELP nexusflow_dag_step_duration_seconds Wall-clock duration of one DAG node.\n")
	sb.WriteString("# TYPE nexusflow_dag_step_duration_seconds gauge\n")
	sb.WriteString("# HELP nexusflow_dag_step_exit_code Exit code of DAG node subprocess (-1 if launch failed).\n")
	sb.WriteString("# TYPE nexusflow_dag_step_exit_code gauge\n")
	for _, st := range steps {
		sec := st.Duration.Seconds()
		id := promEscLabel(st.ID)
		fmt.Fprintf(&sb, `nexusflow_dag_step_duration_seconds{pipeline="%s",step="%s"} %g`+"\n", pl, id, sec)
		fmt.Fprintf(&sb, `nexusflow_dag_step_exit_code{pipeline="%s",step="%s"} %d`+"\n", pl, id, st.ExitCode)
	}
	_, err := io.WriteString(w, sb.String())
	return err
}

// WritePrometheusTextFile truncates path and writes exposition text (node_exporter textfile collector compatible).
func WritePrometheusTextFile(path string, pipeline string, steps []StepTiming) error {
	if path == "" {
		return nil
	}
	var sb strings.Builder
	if err := WritePrometheusText(&sb, pipeline, steps); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}
