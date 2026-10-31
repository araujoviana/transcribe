import subprocess

def sh(cmd, check=True):
    p = subprocess.Popen(cmd, shell=True, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    for line in p.stdout:
        print(line.rstrip(), flush=True)
    rc = p.wait()
    if rc and check:
        raise RuntimeError(f"falhou ({rc}): {cmd}")
    return rc

sh("nvidia-smi -L")
sh("ffmpeg -version | head -1")

# whisperx exige a pilha torch 2.8; o torch do Colab (2.11) quebra o pyannote (torchaudio sem list_audio_backends etc.).
# uv --no-config instala em ~1 min; pip leva ~5 min.
pkgs = "whisperx 'torch~=2.8.0' 'torchaudio~=2.8.0' 'torchvision~=0.23.0'"
if sh(f"uv pip install --system --no-config -q {pkgs}", check=False) != 0:
    print("uv falhou; usando pip (mais lento)", flush=True)
    sh(f"pip install -q {pkgs}")

sh("python -c \"import torch, whisperx.transcribe; assert torch.cuda.is_available(), 'sem CUDA'\"")
print("TRANSCRIBE_OK", flush=True)
