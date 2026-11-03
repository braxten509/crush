# Downloaded local models

In Crush Desktop's model menu, choose **Local models** in the provider column.
Each downloaded GGUF model shows its name, total file size, and **Load** or
**Unload**. Long names truncate before the size and action; hover to read the
full name. Paths are not shown.

**Load** starts the standalone `llama-server` process and shows loading status.
Once its health check succeeds, Crush selects that model for the current chat
(or the next chat if no chat is open). A failed load shows the server error and
leaves the previous chat selection unchanged. Loading another model stops the
previous managed process first. **Unload** stops the managed process and frees
its GPU/RAM allocations; it can also cancel a load. A later send on an unloaded
model asks the user to load it. Selecting a row alone does not load weights.

This is a normal Crush provider, independent of `re-sandbox`. Unsloth Studio,
its passcode, and its vault are not used. The existing standalone llama.cpp
binary under `.unsloth/llama.cpp` can be used while the vault stays locked.

Discovery searches `~/models`, the Hugging Face cache, LM Studio model folders,
and `.unsloth/llama.cpp/models`. `CRUSH_LOCAL_MODEL_DIRS` overrides the search
with a platform path list (colon-separated on Linux). Projector and vocabulary
files are omitted, aliases are deduplicated, and complete numbered GGUF shards
appear as one entry with their combined size. Vault paths and aliases into the
vault are excluded. Other weight formats require conversion or a different
serving engine; compatibility depends on the installed llama.cpp build.

`CRUSH_LLAMA_SERVER` overrides executable discovery. The defaults also check
`~/.local/bin/llama-server`, `.unsloth/llama.cpp/llama-server`, `/usr/bin` and
`/usr/local/bin`. The initial settings are a 32,768-token context, one parallel
slot, and automatic GPU offload/flash attention. Crush disables llama.cpp's own
agent tools and does not pass inherited provider secrets or `LLAMA_ARG_*` flags
to the process. Crush's own tools and permissions continue to apply.

Default provider configurations discover the local provider automatically when
weights are present. With `disable_default_providers`, opt in explicitly:

```json
{"providers":{"local-gguf":{"type":"openai-compat","discover_models":false}}}
```

The engine owns the model process. Local management routes accept only its Unix
socket callers; remote HTTP callers cannot load, unload, or enumerate weights.
A loopback inference relay checks the requested model ID and refuses to serve
one model's chat with different loaded weights. It permits inference routes
only. Closing the engine unloads its model (Linux also kills the child if the
engine dies). Closing a window alone can leave a shared engine running: use
**Unload** when finished. Existing independently started model servers are left
alone. Additional downloads appear after the provider config is reloaded.

Validation uses a CPU-only fixture server: discovery, complete/incomplete
shards, aliases, vault exclusion, load/switch/unload, cancellation, failure,
relay authentication and model identity, local-only routes, and the rendered
desktop controls. No real weights or GPU are required by these checks.
