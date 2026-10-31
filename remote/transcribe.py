import os, subprocess

audio, model = os.environ["T_AUDIO"], os.environ["T_MODEL"]
os.makedirs("/content/out", exist_ok=True)
# Sem --language: detecção automática. Sem diarização. --no_align: o .txt não precisa de timestamps por palavra.
cmd = ["whisperx", audio, "--model", model, "--device", "cuda", "--compute_type", "float16",
       "--no_align", "--output_format", "txt", "--output_dir", "/content/out"]
# torch>=2.6 usa weights_only=True e rejeita o checkpoint do VAD (pyannote) que vem dentro do pacote whisperx.
env = dict(os.environ, TORCH_FORCE_NO_WEIGHTS_ONLY_LOAD="1")
p = subprocess.Popen(cmd, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, bufsize=0)
buf = b""
while True:
    chunk = p.stdout.read(256)
    if not chunk:
        break
    buf += chunk
    parts = buf.replace(b"\r", b"\n").split(b"\n")
    buf = parts.pop()
    for part in parts:
        if part.strip():
            print(part.decode(errors="replace"), flush=True)
if p.wait():
    raise RuntimeError(f"whisperx saiu com código {p.returncode}")
base = os.path.splitext(os.path.basename(audio))[0]
if not os.path.exists(f"/content/out/{base}.txt"):
    raise RuntimeError("whisperx não gerou o .txt")
print("TRANSCRIBE_OK", flush=True)
