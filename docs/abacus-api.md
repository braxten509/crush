# Abacus API in Crush

The `abacus` provider uses RouteLLM and Crush's native agent tools. Its model catalog supplies context limits, token prices, images, and supported API formats. Effort profiles come from Crush's bundled model catalog, with profiles for newer Claude and GPT families.

| Control | Abacus behavior |
| --- | --- |
| Model picker | Includes text models with tools, including Responses-only models. Large and small models can use different API formats. |
| `/effort` | Shows the selected model's levels. Claude uses Messages `output_config.effort`; Responses models use `reasoning.effort`; compatible chat models use `reasoning_effort`. |
| Thinking toggle | Available for models with a supported on/off setting and no effort selector. Models with automatic thinking do not get an ineffective toggle. |
| `/fast` | Uses OpenAI priority processing on supported Abacus models. It is not Claude Code's FAST mode. |
| Ctrl+B | Moves a native foreground shell command to the background. It remains visible in Background Processes and survives canceling the agent turn. |
| Esc / Ctrl+Enter | Uses Crush's native cancel and interrupt handling. Queued messages are incorporated between model steps. |
| `/compact`, titles, images, MCP, permissions and hooks | Uses the existing native agent implementations. |
| `crush spawn --cli abacus --model MODEL --effort LEVEL` | Starts a native API sub-agent with Crush tools and a child session. `--fast` is accepted on supported priority models. |
| Model configuration | Keeps maximum output tokens, sampling controls where supported, provider options, extra headers and body fields, and request timeouts. Effort and FAST selections survive model resolution and reload. |

Claude models use `/v1/messages`, Responses-capable models use `/v1/responses`, and other models use `/v1/chat/completions`. The public provider ID stays `abacus` across those routes.

The private key is referenced from the user's configuration. It is never included in source or model instructions. Instruction files remain disabled when `disable_instruction_files` is enabled; the native agent receives Crush's shared instructions, memory, and shared skills instead.

Tests capture outgoing requests to verify parameter names and routes, check model-specific command availability and configuration persistence, and exercise native Ctrl+B and background-process discovery. The API was also checked with live Claude and GPT requests and the effort and FAST controls were checked in a headless TUI.
