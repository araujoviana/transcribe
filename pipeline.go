package main

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

//go:embed remote/install.py
var installPy []byte

//go:embed remote/transcribe.py
var transcribePy []byte

type step int

const (
	stPrepare step = iota
	stCreate
	stInstall
	stUpload
	stTranscribe
	stDownload
	stCleanup
	numSteps
)

type reporter interface {
	Start(step)
	Detail(step, string)
	Done(step)
}

// stepError diz em qual etapa o erro ocorreu e guarda o final da saída do comando.
type stepError struct {
	step step
	err  error
	tail string
}

func (e *stepError) Error() string { return fmt.Sprintf("%s: %v", stepLabel(e.step, false), e.err) }
func (e *stepError) Unwrap() error { return e.err }

type input struct {
	raw   string
	isURL bool
}

func classify(arg string) (input, error) {
	if strings.Contains(arg, "://") || strings.HasPrefix(arg, "youtu") || strings.HasPrefix(arg, "www.") {
		raw := arg
		if !strings.Contains(raw, "://") {
			raw = "https://" + raw
		}
		u, err := url.Parse(raw)
		if err != nil {
			return input{}, fmt.Errorf("URL inválida: %w", err)
		}
		h := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
		switch h {
		case "youtube.com", "m.youtube.com", "music.youtube.com", "youtu.be", "youtube-nocookie.com":
			return input{raw: raw, isURL: true}, nil
		}
		return input{}, fmt.Errorf("só URLs do YouTube são suportadas (recebido: %s)", u.Hostname())
	}
	st, err := os.Stat(arg)
	if err != nil {
		return input{}, fmt.Errorf("arquivo não encontrado: %s", arg)
	}
	if st.IsDir() {
		return input{}, fmt.Errorf("%s é um diretório", arg)
	}
	return input{raw: arg}, nil
}

type pipeline struct {
	cfg    config
	in     input
	outDir string
	rep    reporter
}

func (p *pipeline) fail(s step, err error, res cmdResult) error {
	if err == nil {
		return nil
	}
	if ctx := errors.Is(err, context.Canceled); ctx {
		return &stepError{step: s, err: errors.New("cancelado pelo usuário")}
	}
	return &stepError{step: s, err: err, tail: res.Tail(12)}
}

var unsafeName = regexp.MustCompile(`[^\p{L}\p{N}._ -]+`)

func safeName(s string) string {
	s = strings.TrimSpace(unsafeName.ReplaceAllString(s, "_"))
	s = strings.Trim(s, "._ ")
	if len(s) > 120 {
		s = s[:120]
	}
	if s == "" {
		s = "transcricao"
	}
	return s
}

func uniquePath(dir, base string) string {
	p := filepath.Join(dir, base+".txt")
	for i := 2; ; i++ {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return p
		}
		p = filepath.Join(dir, fmt.Sprintf("%s-%d.txt", base, i))
	}
}

// prepare devolve o caminho local do áudio e o nome-base para o .txt.
func (p *pipeline) prepare(ctx context.Context, tmp string) (audio, base string, err error) {
	p.rep.Start(stPrepare)
	defer func() {
		if err == nil {
			p.rep.Done(stPrepare)
		}
	}()
	if !p.in.isURL {
		name := filepath.Base(p.in.raw)
		return p.in.raw, safeName(strings.TrimSuffix(name, filepath.Ext(name))), nil
	}
	// Baixa localmente: IPs de datacenter do Colab costumam ser bloqueados pelo YouTube.
	res, err := run(ctx, func(l string) { p.rep.Detail(stPrepare, l) }, "yt-dlp", "--no-playlist", "--newline", "-x",
		"--print-to-file", "%(title)s", filepath.Join(tmp, "title.txt"),
		"-o", filepath.Join(tmp, "audio.%(ext)s"), p.in.raw)
	if err != nil {
		return "", "", p.fail(stPrepare, err, res)
	}
	matches, _ := filepath.Glob(filepath.Join(tmp, "audio.*"))
	if len(matches) == 0 {
		return "", "", p.fail(stPrepare, errors.New("yt-dlp não gerou arquivo de áudio"), res)
	}
	title := "youtube"
	if b, err := os.ReadFile(filepath.Join(tmp, "title.txt")); err == nil {
		title = string(b)
	}
	return matches[0], safeName(title), nil
}

func (p *pipeline) colab(ctx context.Context, s step, args ...string) (cmdResult, error) {
	return run(ctx, func(l string) { p.rep.Detail(s, l) }, p.cfg.Colab, args...)
}

// Run executa o fluxo inteiro. A VM é sempre destruída antes de retornar.
func (p *pipeline) Run(ctx context.Context) (out string, err error) {
	tmp, err := os.MkdirTemp("", "transcribe-")
	if err != nil {
		return "", &stepError{step: stPrepare, err: err}
	}
	defer os.RemoveAll(tmp)

	var rnd [3]byte
	_, _ = rand.Read(rnd[:])
	sess := "transcribe-" + hex.EncodeToString(rnd[:])

	// Registra a sessão ANTES de criar, para que um kill -9 deixe rastro recuperável na próxima execução.
	if err := saveActive(sess); err != nil {
		return "", &stepError{step: stCreate, err: err}
	}
	defer func() {
		// Contexto novo: o cleanup precisa rodar mesmo após Ctrl+C.
		cctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		p.rep.Start(stCleanup)
		if cerr := p.cleanup(cctx, sess); cerr != nil {
			cerr = &stepError{step: stCleanup, err: cerr}
			if err == nil {
				err = cerr
			} else {
				err = errors.Join(err, cerr)
			}
			return
		}
		p.rep.Done(stCleanup)
	}()

	// Grafo: criar VM -> instalar  ||  baixar áudio -> (VM pronta) -> enviar. Ambos juntam em transcrever.
	gctx, gcancel := context.WithCancel(ctx)
	defer gcancel()
	var (
		wg       sync.WaitGroup
		once     sync.Once
		firstErr error
		created  = make(chan struct{})
		audio    string
		base     string
	)
	setErr := func(e error) {
		once.Do(func() { firstErr = e; gcancel() })
	}
	remote := ""
	wg.Add(2)
	go func() {
		defer wg.Done()
		p.rep.Start(stCreate)
		if res, err := p.colab(gctx, stCreate, "new", "-s", sess, "--gpu", p.cfg.GPU); err != nil {
			setErr(p.fail(stCreate, err, res))
			return
		}
		p.rep.Done(stCreate)
		close(created)
		if err := p.remoteScript(gctx, stInstall, sess, installPy, tmp, "1800"); err != nil {
			setErr(err)
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		audio, base, err = p.prepare(gctx, tmp)
		if err != nil {
			setErr(err)
			return
		}
		select {
		case <-created:
		case <-gctx.Done():
			return
		}
		p.rep.Start(stUpload)
		remote = "/content/audio" + strings.ToLower(filepath.Ext(audio))
		if res, err := p.colab(gctx, stUpload, "upload", "-s", sess, audio, remote); err != nil {
			setErr(p.fail(stUpload, err, res))
			return
		}
		p.rep.Done(stUpload)
	}()
	wg.Wait()
	if firstErr != nil {
		return "", firstErr
	}
	if ctx.Err() != nil {
		return "", &stepError{step: stTranscribe, err: errors.New("cancelado pelo usuário")}
	}

	if err := p.remoteScript(ctx, stTranscribe, sess, transcribePy, tmp, "7200", "T_AUDIO="+remote, "T_MODEL="+p.cfg.Model); err != nil {
		return "", err
	}

	p.rep.Start(stDownload)
	remoteTxt := "/content/out/" + strings.TrimSuffix(filepath.Base(remote), filepath.Ext(remote)) + ".txt"
	local := filepath.Join(tmp, "result.txt")
	if res, err := p.colab(ctx, stDownload, "download", "-s", sess, remoteTxt, local); err != nil {
		return "", p.fail(stDownload, err, res)
	}
	out = uniquePath(p.outDir, base)
	if err := moveFile(local, out); err != nil {
		return "", &stepError{step: stDownload, err: err}
	}
	p.rep.Done(stDownload)
	return out, nil
}

func (p *pipeline) remoteScript(ctx context.Context, s step, sess string, script []byte, tmp, timeout string, env ...string) error {
	p.rep.Start(s)
	f := filepath.Join(tmp, fmt.Sprintf("step%d.py", s))
	if err := os.WriteFile(f, script, 0o600); err != nil {
		return &stepError{step: s, err: err}
	}
	args := []string{"exec", "-s", sess, "-f", f, "--timeout", timeout}
	for _, e := range env {
		args = append(args, "--env", e)
	}
	res, err := p.colab(ctx, s, args...)
	if err == nil && !res.Has("TRANSCRIBE_OK") {
		err = errors.New("script remoto terminou sem sucesso")
	}
	if err != nil {
		return p.fail(s, err, res)
	}
	p.rep.Done(s)
	return nil
}

// cleanup para a sessão e confirma no servidor que ela sumiu.
func (p *pipeline) cleanup(ctx context.Context, sess string) error {
	return stopSession(ctx, p.cfg.Colab, sess, func(l string) { p.rep.Detail(stCleanup, l) })
}

func stopSession(ctx context.Context, colab, sess string, onLine func(string)) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		_, lastErr = run(ctx, onLine, colab, "stop", "-s", sess)
		res, serr := run(ctx, nil, colab, "sessions")
		if serr == nil {
			alive := false
			for _, l := range res.Lines {
				if strings.Contains(l, sess) {
					alive = true
				}
			}
			if !alive {
				clearActive()
				return nil
			}
			lastErr = fmt.Errorf("sessão %s ainda aparece ativa", sess)
		} else if lastErr == nil {
			lastErr = fmt.Errorf("não consegui confirmar o encerramento: %w", serr)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("VM %s pode continuar ligada, rode: colab stop -s %s (%v)", sess, sess, lastErr)
		case <-time.After(3 * time.Second):
		}
	}
	return fmt.Errorf("VM %s pode continuar ligada, rode: colab stop -s %s (%v)", sess, sess, lastErr)
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}
