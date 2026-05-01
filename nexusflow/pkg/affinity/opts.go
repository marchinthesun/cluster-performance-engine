package affinity

// RunOpts configures optional Linux behaviors applied in the locked thread before exec.
type RunOpts struct {
	// Nice, if non-nil, is applied with setpriority(PRIO_PROCESS, 0, *Nice) before sched_setaffinity.
	// Range is typically -20..19; values < 0 often require CAP_SYS_NICE or superuser.
	Nice *int
}
