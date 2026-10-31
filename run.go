package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

var tokenRe = regexp.MustCompile(`token=[A-Za-z0-9._-]+`)

// colab CLI avisa sobre atualização a cada chamada; isso é ruído para a UI.
func isColabNoise(l string) bool {
	return strings.HasPrefix(l, "[colab] A new version") ||
		strings.HasPrefix(l, "[colab] Run 'colab update'") ||
		strings.HasPrefix(l, "[colab] To silence this check")
}

// scanLines divide em \n e \r, para que barras de progresso (tqdm, yt-dlp) virem linhas.
func scanLines(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

type cmdResult struct {
	Lines []string // saída completa, sem ruído
}

func (r cmdResult) Tail(n int) string {
	if len(r.Lines) > n {
		return strings.Join(r.Lines[len(r.Lines)-n:], "\n")
	}
	return strings.Join(r.Lines, "\n")
}

func (r cmdResult) Has(s string) bool {
	for _, l := range r.Lines {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}

// run executa um comando, junta stdout+stderr e chama onLine para cada linha.
func run(ctx context.Context, onLine func(string), name string, args ...string) (cmdResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	pr, pw := newPipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		return cmdResult{}, err
	}
	var res cmdResult
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		sc.Split(scanLines)
		for sc.Scan() {
			l := tokenRe.ReplaceAllString(strings.TrimSpace(sc.Text()), "token=REDACTED")
			if l == "" || isColabNoise(l) {
				continue
			}
			res.Lines = append(res.Lines, l)
			if onLine != nil {
				onLine(l)
			}
		}
	}()
	err := cmd.Wait()
	pw.Close()
	wg.Wait()
	pr.Close()
	if err != nil {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return res, fmt.Errorf("%s saiu com código %d", name, ee.ExitCode())
		}
	}
	return res, err
}
