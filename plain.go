package main

import (
	"errors"
	"fmt"
	"os"
	"time"
)

func isTTY(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// plainReporter é o fallback sem TTY (pipe, CI, cron): uma linha por evento, em stderr.
type plainReporter struct {
	begin   time.Time
	started [numSteps]time.Time
	isURL   bool
}

func (r *plainReporter) Start(s step) {
	r.started[s] = time.Now()
	fmt.Fprintf(os.Stderr, "[%6s] ▶ %s\n", fmtDur(time.Since(r.begin)), stepLabel(s, r.isURL))
}
func (r *plainReporter) Done(s step) {
	fmt.Fprintf(os.Stderr, "[%6s] ✓ %s (%s)\n", fmtDur(time.Since(r.begin)), stepLabel(s, r.isURL), fmtDur(time.Since(r.started[s])))
}
func (r *plainReporter) Detail(s step, l string) {
	fmt.Fprintf(os.Stderr, "           [%d] %s\n", s, l)
}

func (r *plainReporter) finish(out string, err error) {
	if err == nil {
		fmt.Fprintf(os.Stderr, "Pronto: %s\n", out)
		return
	}
	fmt.Fprintf(os.Stderr, "Erro: %v\n", err)
	var se *stepError
	if errors.As(err, &se) && se.tail != "" {
		fmt.Fprintln(os.Stderr, indent(se.tail))
	}
}
