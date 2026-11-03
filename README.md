# transcribe

Script interno para transcrever vídeos do YouTube ou arquivos de áudio/vídeo com WhisperX (large-v3) numa GPU do Google Colab. Gera um `.txt` no diretório atual.

Feito pra uso pessoal

## Requisitos

- Go
- [`colab` CLI](https://github.com/googlecolab/google-colab-cli) instalado e autenticado (`uv tool install google-colab-cli`)
- `yt-dlp` e `ffmpeg` no PATH
- Conta Google com acesso a GPU no Colab

## Instalar

```
go build -o ~/.local/bin/transcribe .
```

## Uso

```
transcribe [-gpu T4] [-model large-v3] [-o dir] <url-do-youtube | arquivo>
```

```
transcribe 'https://www.youtube.com/watch?v=ID'
transcribe ./aula.mp4
```

Use aspas na URL no zsh por causa do `?`.

Idioma detectado automaticamente, sem diarização, saída em texto simples. Se o arquivo de saída já existir, cria `nome-2.txt`.

A VM é destruída no fim, inclusive quando há erro ou Ctrl+C. Se o processo for morto à força, a próxima execução destrói a VM que sobrou.

## Configuração

Opcional, em `~/.config/transcribe/config.json`:

```json
{ "colab": "colab", "gpu": "T4", "model": "large-v3" }
```

A autenticação fica com o próprio `colab` (`~/.config/colab-cli`). Este projeto não guarda credenciais.

## Aviso

O FAQ do Colab não diz nada sobre uso automatizado. Use por sua conta e risco.

## Licença

MIT
