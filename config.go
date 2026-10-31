package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Tudo fica em ~/.config/transcribe/. O token OAuth do Colab é gerenciado pelo
// próprio CLI (~/.config/colab-cli); aqui só há preferências e o estado da sessão ativa.
type config struct {
	Colab string `json:"colab"` // caminho do executável colab
	GPU   string `json:"gpu"`
	Model string `json:"model"`
}

func configDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(d, "transcribe")
}

func loadConfig() config {
	c := config{Colab: "colab", GPU: "T4", Model: "large-v3"}
	if b, err := os.ReadFile(filepath.Join(configDir(), "config.json")); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func activeFile() string { return filepath.Join(configDir(), "active_session") }

func saveActive(name string) error {
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return err
	}
	return os.WriteFile(activeFile(), []byte(name), 0o600)
}

func clearActive() { _ = os.Remove(activeFile()) }

func readActive() string {
	b, err := os.ReadFile(activeFile())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
