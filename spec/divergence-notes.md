# Divergence Notes — arkannie vs annie2/arkannie specs

> **HISTÓRICO — no normativo.** Este documento registra las divergencias annie2→arkannie
> tal como se planificaron **antes** de la spec nativa v0.2. Desde que `ann-lang.md` se
> reescribió como especificación **nativa y normativa** de Ann v0.2, la autoridad sobre la
> sintaxis y semántica del lenguaje reside allí (y en `agent-protocol.md` para el envelope);
> este archivo ya **no** es normativo. Se conserva como registro de decisiones para
> trazabilidad. La afirmación de abajo de que `ann-lang.md` es "copia verbatim de annie2 y
> sigue normativa" quedó obsoleta con esa reescritura: léase en clave histórica.

Las specs en esta carpeta (`ann-lang.md`, `agent-protocol.md`, `agent-schema.yaml`) son copias
verbatim de `annie2/.arkannie/` y siguen siendo **normativas**. Esta nota documenta las divergencias
controladas que la nueva arkannie introduce. Todo lo no listado aquí aplica tal cual.

## Arquitectura de ejecución

| annie2 | arkannie (nueva) |
|---|---|
| Arkannie es una sesión Claude (Level 1) que interpreta Ann | Arkannie es un **binario Go** que compila/interpreta Ann de forma determinista; Claude solo ejecuta agentes |
| Waves despachados vía Agent() dentro de la sesión | Waves = procesos `claude -p` aislados, spawn del binario |
| Modo interactivo + batch | Batch-only vía `arkannie`; superficie conversacional solo en `--forge` e intérprete `--interpret` |
| `[mem]` / `[personality]` comandos nativos | Eliminados. `.mem/` es memoria exclusiva del runtime (checkpoints §10, run-dirs, healthcheck cache), inaccesible a agentes. Personalities son capa de render (campo `personality:` en agent.yaml) |

## agent-schema.yaml — campos

**Nuevos (obligatorios):**
- `model: haiku | sonnet | opus` — VAL-10. Modelo Claude por agente.
- `scope: agnostic | executor` — VAL-11. Clase de contención (doble llave con `--allow-workspace`).
- `capabilities: { purpose, use_when, [inputs, produces, examples] }` — VAL-18. La **carta de presentación** del agente para el catálogo: `purpose` (qué resuelve) y `use_when` (cuándo elegirlo) son obligatorios; `inputs`/`produces`/`examples` enriquecen. El binario la lee y la presenta con `--catalog` para que la IA superior seleccione agentes. Ver `arkannie-catalog`.

**Nuevos (opcionales):**
- `personality:` — bloque **inline** en `agent.yaml`: `default` (texto de la sección por defecto) + `values` (mapa `<persona> → texto`), seleccionado por `--personality=<persona>`. No hay archivos `.agents/.personalities/`.
- `default_operation: <op>` — operación usada cuando el dispatch no trae flag de operación (obligatoria para modo prompt libre).
- `layer: { origin: <ruta-abs> }` — marca un **agente layer** (absorción `<absorb> --mode=layer`): el runtime ensambla el harness igual que siempre pero spawnea claude con `cwd = origin` (la AI original carga su propia identidad nativamente) y entrega el harness vía `--append-system-prompt-file`. La AI original nunca se modifica. Ver `arkannie-absorb.md`.

**Eliminados:**
- `command_type: wave | native` — todo lo que vive en `.agents/` es wave; lo nativo está compilado en el binario.

**Reglas VAL nuevas:**
- VAL-10: `model` presente y ∈ {haiku, sonnet, opus}.
- VAL-11: `scope` presente y ∈ {agnostic, executor}.
- VAL-12: si `scope: agnostic` → `grants` ⊆ {read, network} (sin write/execute). Los agentes v1 son estáticos: función pura input→envelope.
- VAL-17: si el agente declara `layer:` → `origin` debe ser ruta absoluta, directorio existente y legible, con un `CLAUDE.md` de identidad, y que no solape el root de arkannie (anti-recursión). Falla → se excluye solo ese agente; el resto carga normal.
- VAL-18: todo agente debe declarar `capabilities:` con `purpose` y `use_when` no vacíos. Falla → se excluye solo ese agente. Los generadores (Forge y `<absorb>`) emiten la carta, así que ningún agente forjado o absorbido nace inválido.

## CLI — consulta de catálogo (`cmd/arkannie`)

- `--catalog[=<agente>]` — valor opcional, aceptado solo en forma `=` (nunca consume el siguiente token). Carga el registry e imprime la carta de presentación de cada agente válido (o de uno solo), en texto legible tipo `--help`, para consumo de la IA superior/orquestador. Exit 0; agente pedido inexistente → 64; errores de carga a stderr sin suprimir los agentes válidos. No ejecuta programa ni prompt.
- `--man[=<agente>]` — hermano de `--catalog`, misma mecánica de flag (opcional, solo forma `=`, no consume el siguiente token). Imprime el **manual de uso de grado ejecución** en **Markdown**: regla de dispatch por scope (executor→`--allow-workspace`; agnostic→read-only/`--path` absoluto; layer→origin+`--allow-layer`), modos de invocación, y por operación el contrato completo (context, grants, flags, groups, modifiers, `output_schema` success/info/error), personalities, Ask Protocol y ejemplos (los de `capabilities.examples` + uno sintetizado por operación). Derivado **puro del registry**, determinista, sin LLM. Donde `--catalog` da lo justo para *seleccionar* un agente, `--man` da lo suficiente para *ejecutarlo* end-to-end. Exit 0; inexistente → 64.

## CLI — familia forge/absorb (`cmd/arkannie`)

`arkannie` gana cuatro banderas para la absorción de AIs externas (extensión del Forge; ver `arkannie-absorb.md`):
- `--forge[=<agente>]` — valor opcional, aceptado solo en forma `=` (nunca consume el siguiente token). Sin valor: wizard clásico; con valor: forge sobre un agente nombrado.
- `--absorb=<ruta>` — requiere `--forge`; ruta de la AI a absorber (pre-validada existe/dir/legible, resuelta absoluta contra el cwd del invocador antes de spawnear claude).
- `--mode=<complete|fragment|layer>` — requiere `--absorb`; preferencia-semilla global de estrategia (la conversación en F3 puede contradecirla).
- `--allow-layer[=<n,n>]` — consentimiento de ejecución para agentes layer (built-in del runtime, superficie de confianza distinta de `--allow-workspace`): bare habilita todos los layer del programa; con lista, solo los nombrados. Un dispatch de agente layer sin este consentimiento → Class B atrapable (`error -> {}`). Se transporta a todos los call sites de spawn (programa, `parallel`, `--agent`, re-exec `--detach`).

Composición inválida (`--absorb` sin `--forge`; `--mode` sin `--absorb`; `--mode` fuera del enum; nombre de `--forge=` fuera de `^[a-z][a-z0-9-]*$`; `--forge=` vacío; lista `--allow-layer` malformada) → exit 64 sin cargar el registry.

## Empaquetado de agentes

annie2: un archivo `[name].annspec.md` (frontmatter + harness) en `.arkannie/` plano.
arkannie: carpeta `.agents/<nombre>/` con `agent.yaml` (contrato, validado al arranque) + `harness.md`
(template puro). Sin CLAUDE.md por agente — el binario entrega el prompt renderizado completo.

## Registro de comandos

El `[new-command]` wizard (agent-schema.yaml, bloque final) se reencarna como **Agent Forge**
(`arkannie --forge`): mismo flujo 5 fases, con gate determinista `arkannie validate --agent=X`.
El forge es el único escritor de `.agents/`.

## Diferido

- `.knowledge/` (aprendizaje persistente por agente) — documentado en el brief, fuera de v1.
- Agentes `scope: executor` — el mecanismo de doble llave queda implementado; los agentes se
  añadirán después de v1.
