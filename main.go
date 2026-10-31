package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	cfg := loadConfig()
	gpu := flag.String("gpu", cfg.GPU, "GPU do Colab (T4, L4, G4, A100, H100)")
	modelName := flag.String("model", cfg.Model, "modelo whisper")
	outDir := flag.String("o", ".", "diretório de saída")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "uso: transcribe [-gpu T4] [-model large-v3] [-o dir] <url-youtube | arquivo>")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	cfg.GPU, cfg.Model = *gpu, *modelName

	in, err := classify(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(2)
	}
	for _, bin := range []string{cfg.Colab, "ffmpeg"} {
		if _, err := lookPath(bin); err != nil {
			fmt.Fprintf(os.Stderr, "erro: %q não encontrado no PATH (uv tool install google-colab-cli)\n", bin)
			os.Exit(2)
		}
	}
	if in.isURL {
		if _, err := lookPath("yt-dlp"); err != nil {
			fmt.Fprintln(os.Stderr, "erro: yt-dlp não encontrado no PATH")
			os.Exit(2)
		}
	}

	// Sessão deixada por uma execução anterior morta (kill -9, queda de energia): destrói antes de seguir.
	if old := readActive(); old != "" {
		fmt.Fprintf(os.Stderr, "limpando VM órfã %s de execução anterior…\n", old)
		cctx, c := context.WithTimeout(context.Background(), 2*time.Minute)
		err := stopSession(cctx, cfg.Colab, old, nil)
		c()
		if err != nil {
			fmt.Fprintln(os.Stderr, "erro:", err)
			os.Exit(1)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var p *tea.Program
	var rep reporter
	tty := isTTY(os.Stdout) && isTTY(os.Stdin)
	if tty {
		p = tea.NewProgram(newModel(in.isURL, cancel), tea.WithoutSignalHandler())
		rep = teaReporter{p}
	} else {
		rep = &plainReporter{begin: time.Now(), isURL: in.isURL}
	}

	// SIGINT/SIGTERM/SIGHUP externos também disparam o cancelamento (e portanto o cleanup).
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for range sigs {
			cancel()
		}
	}()

	pl := &pipeline{cfg: cfg, in: in, outDir: *outDir, rep: rep}
	var plainErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		out, err := pl.Run(ctx)
		if tty {
			p.Send(finishMsg{out: out, err: err})
		} else {
			rep.(*plainReporter).finish(out, err)
			plainErr = err
		}
	}()

	if !tty {
		<-done
		if plainErr != nil {
			os.Exit(1)
		}
		return
	}
	final, runErr := p.Run()
	if runErr != nil {
		cancel()
	}
	<-done // garante que o cleanup terminou antes de sair
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "erro na UI:", runErr)
		os.Exit(1)
	}
	if final.(model).err != nil {
		os.Exit(1)
	}
}
