# arkannie — Manual de usuario

**arkannie** es un *harness stateless de agentes de IA*. El binario Go (`arkannie`)
compila y ejecuta programas escritos en **Ann** (un lenguaje de despacho
minimalista), orquestando agentes que corren como procesos `claude` aislados.
El binario es determinista: no hay LLM en el camino de compilación/despacho —
Claude solo ejecuta los agentes.

Versión: `0.1.0` · Lenguaje: Ann v0.1

> Referencia rápida en cualquier momento con `arkannie --help`. Este manual es
> la versión extendida.

---

## 1. Instalación y empaquetado

### Requisitos
- Go 1.21+ (para compilar).
- El CLI de `claude` en el `PATH` (o configurado en `arkannie.config.yaml`) para
  ejecutar agentes de verdad. Los tests no lo requieren (usan stubs).

### Compilar e instalar
```bash
make build        # compila bin/arkannie
make install      # symlinkea bin/arkannie.sh → $PREFIX/bin/arkannie (PREFIX=~/.local)
make test         # gofmt + vet + go test -race -cover
```

`make install` deja en el `PATH` el **shim** `arkannie`, que resuelve
`ARKANNIE_HOME` (la raíz que contiene `bin/arkannie`, `.agents/`, `.mem/`, `.output/`)
y ejecuta el binario compilado.

> **Gotcha operativo:** el shim ejecuta el binario ya compilado; **no
> recompila**. Tras cualquier cambio de código corre `make build`, o el shim
> seguirá corriendo la versión vieja.

### Empaquetar para distribución
```bash
make dist         # produce dist/arkannie-<version>.tar.gz
```

El tarball es un `ARKANNIE_HOME` autocontenido (binario + shim + `.agents/` +
identidad + specs + este manual). En la máquina destino:
```bash
tar -xzf arkannie-0.1.0.tar.gz
ln -sf "$PWD/arkannie-0.1.0/bin/arkannie.sh" ~/.local/bin/arkannie
arkannie --help
```

### Configuración — `arkannie.config.yaml`
Todas las claves son opcionales (se muestran los defaults):
```yaml
# timeout_default: 120   # segundos por invocación de agente (> 0)
# max_concurrency: 4     # procesos claude simultáneos (> 0)
# claude_bin: claude     # binario del CLI de claude (nombre en PATH o ruta)
```

---

## 2. Inicio rápido

```bash
# Modo prompt: texto libre contra un agente
arkannie --agent=echo --id=saludo "hola mundo"

# Modo programa: un archivo .ann (cada dispatch nombra su propio agente)
arkannie --id=corrida programa.ann

# Ver qué agentes hay y qué hacen
arkannie --catalog

# Validar los contratos de agente
arkannie validate
```

Toda ejecución escribe su resultado en `.output/<id>.md` e imprime la ruta.

---

## 3. Referencia de la CLI

### Modos de ejecución
| Invocación | Modo |
|---|---|
| `arkannie --id=<id> prog.ann` | **Programa** — ejecuta un `.ann`; cada dispatch resuelve su agente |
| `arkannie --agent=<n> --id=<id> "texto"` | **Prompt** — texto libre contra un solo agente |

`--id` es **obligatorio** en toda ejecución: nombra el archivo de salida
`.output/<id>.md`. La corrida más reciente conserva el nombre limpio; en
colisión, la anterior se archiva a `.output/<id>-N.md`.

### Banderas y subcomandos
| Bandera | Efecto |
|---|---|
| `--detach` | imprime la ruta de salida y corre en segundo plano |
| `--interpret` | ante un error de parse, pide a claude reparar el programa **una** vez |
| `--allow-workspace` | permite a agentes `executor` escribir en el cwd del invocador |
| `--forge[=nombre]` | abre una sesión interactiva del Agent Forge; `=nombre` apunta a un agente existente (valor solo en forma `=`) |
| `--absorb=<ruta>` | absorbe una AI externa en la sesión del Forge (requiere `--forge`) |
| `--mode=<complete\|fragment\|layer>` | estrategia de absorción (requiere `--absorb`) |
| `--allow-layer[=n,n]` | consentimiento para despachar agentes *layer* (todos, o solo los nombrados; valor solo en forma `=`) |
| `--catalog[=agente]` | imprime el catálogo de capacidades — la carta de cada agente, o la de uno solo (valor solo en forma `=`) |
| `validate [--agent=<n>]` | valida los contratos bajo `.agents/` |
| `--version` | imprime la versión de arkannie y sale |
| `--help`, `-h` | imprime el tutorial |

### Códigos de salida
`0` éxito · `1` error · `2` info · `64` error de uso

---

## 4. El lenguaje Ann (v0.1)

Ann es un lenguaje de **despacho**, no de propósito general. Tres niveles
estructurales: **arkannie** (el runtime, Nivel 1), los **wave agents** (Nivel 2,
un proceso claude por dispatch que devuelve un envelope), y los **sub-agents**
(Nivel 3, trabajadores anónimos que un wave crea internamente).

### Estructura del programa
```
# ann v0.1                 // header obligatorio, línea 1, columna 0
// los comentarios son de línea con //
```

### Dispatch — el átomo
```
[seeker] "query" --depth=2 --id=find : contexto verbatim
```
`[command]` es el agente; siguen args posicionales y `--flags`; `--id` etiqueta
el dispatch; todo tras `: ` es contexto verbatim (puede abarcar varias líneas).
Los `$refs` en args o contexto se sustituyen desde RAM.

### Bindings (RAM) y `$result`
```
$x = "una cadena literal"
$items = list("a", "b", $x)
$r = [seeker] : encuéntralo      // $r contiene el payload de éxito
```
Cada bloque `{ }` es un scope: los bindings creados dentro desaparecen al cerrar.

### Handlers trinarios
Todo agente devuelve `success`, `error` o `info`. Se enganchan hasta tres
handlers; dentro, `$result` expone `{id, status, payload}`:
```
[seeker] : busca el config
  success -> { [writer] : usa $result.payload }
  error   -> { [notify] no se encontró }
  info    -> { }
```
Un `error` sin handler de error escala y falla la corrida.

### Control de flujo
```
foreach $items { [worker] : $item }      // iteración secuencial sobre una lista
loop limit=3   { [worker] : reintenta }  // repetición acotada
parallel {                               // despacho concurrente (cada uno --id único)
  [a] --id=one : x
  [b] --id=two : y
}
  each -> { [notify] : $result.payload }
```

### `[return]` — el indicador de salida
El programa decide qué aparece en la salida; los payloads de éxito **no** se
vuelcan automáticamente:
```
[return] $summary               // return único: sin encabezado, solo contenido
[return] --id=result $summary   // sección titulada "## result"
[return] "una nota fija"        // literal, verbatim
```
Un `[return]` toma un operando (un `$binding` o un literal). Con dos o más
returns, cada uno requiere `--id` único; un return dentro de foreach/loop/each
requiere `--id` y emite secciones numeradas (`--id-1`, `--id-2`, …). Un programa
sin `[return]` produce un cuerpo vacío.

### Keywords nativos
| Keyword | Efecto |
|---|---|
| `[ask-user] <texto>` | surface una pregunta; la corrida para con status `info` |
| `[notify] <texto>` | agrega una nota a la sección Notices del reporte |
| `[clarify] <texto>` | igual que notify, para aclaraciones |
| `[return] <operando>` | emite un bloque de salida |

---

## 5. Contratos de agente (`.agents/<nombre>/`)

Cada agente es una carpeta con `agent.yaml` (el contrato, validado al arranque)
y `harness.md` (el template del prompt). El Forge es el **único** escritor de
`.agents/`; el runtime lo trata como ROM.

### `agent.yaml` — campos
| Campo | Obligatorio | Descripción |
|---|---|---|
| `command` | sí (VAL-01) | token bracketed, patrón `[a-z][a-z0-9-]*`, p. ej. `[echo]` |
| `model` | sí (VAL-10) | `haiku` \| `sonnet` \| `opus` |
| `scope` | sí (VAL-11) | `agnostic` (función pura, grants ⊆ {read, network}) \| `executor` (puede escribir en el workspace) |
| `capabilities` | sí (VAL-18) | la **carta de presentación** para el catálogo (ver §6) |
| `operations` | sí (VAL-03) | al menos una operación; cada una con `id`, `description`, `grants`, `output_schema` |
| `default_operation` | no | operación usada cuando el dispatch no trae flag de operación (obligatoria para modo prompt) |
| `personality` | no | bloque `{default, values}`; `--personality=<v>` selecciona una sección |
| `timeout` | no | segundos (> 0); default de config si se omite |
| `layer` | no | `{origin: <ruta-abs>}` — marca un agente *layer* (ver §8) |

**Grants → herramientas:** `read`→Read/Grep/Glob, `write`→Write/Edit,
`execute`→Bash, `network`→WebFetch/WebSearch. Un agente `agnostic` no puede
declarar `write`/`execute` (VAL-12).

**Directivas de flag** (opcionales, por operación): `groups` (excluyentes por
dentro), `modifiers` (combinables), y el `personality` a nivel agente. Requieren
los slots `{{ directives_pre }}`/`{{ directives_post }}` en el harness (VAL-16).

### Validar
```bash
arkannie validate            # todos los agentes; exit 1 si hay violaciones
arkannie validate --agent=echo
```

---

## 6. Catálogo de capacidades (`--catalog`)

Cada agente **provee una carta de presentación** en su bloque `capabilities:`,
para que un consumidor (una IA orquestadora, o el developer) descubra qué
agentes existen y cuándo usarlos.

```yaml
capabilities:
  purpose: Return the dispatched text, optionally reshaped by directive flags.  # obligatorio
  use_when: You need a trivial, deterministic passthrough.                       # obligatorio
  inputs: free-text in context.text        # opcional
  produces: the same text, echoed back     # opcional
  examples:                                 # opcional
    - '[echo] : hola mundo'
```

`purpose` y `use_when` son obligatorios (VAL-18). Consulta:
```bash
arkannie --catalog          # carta de todos los agentes válidos, ordenados
arkannie --catalog=echo     # solo la carta de [echo]
```
Salida legible tipo `--help`, exit 0; agente inexistente → 64. Los generadores
de agentes (Forge y absorción) emiten la carta automáticamente.

---

## 7. Agent Forge (`--forge`)

Sesión interactiva de 5 fases para crear o editar agentes en `.agents/` sin
edición manual: nombre/clase (incl. `capabilities`), operations, output schemas,
borrador (cero escrituras hasta confirmar), y escritura + `validate` + dispatch
smoke.
```bash
arkannie --forge              # wizard clásico
arkannie --forge=seeker       # forge pre-sembrado con el nombre del agente
```

---

## 8. Absorción de AIs externas (`--absorb`)

Convierte una customización de Claude existente (CLAUDE.md, roles, comandos,
knowledge) en agente(s) arkannie, sin destruir la fuente. Protocolo de 7 fases
(ver `arkannie-absorb.md`), con tres estrategias:
- **complete** — 1 agente con N operations.
- **fragment** — N agentes + un `.ann` de recomposición.
- **layer** — el agente conserva `layer.origin`: el runtime spawnea claude con
  `cwd=origin` (la AI carga su identidad nativa) y entrega el harness ensamblado
  como contrato de envelope. La AI original nunca se modifica.

```bash
arkannie --forge=nova --absorb=/ruta/a/la/ai --mode=layer
```
`--absorb` requiere `--forge`; `--mode` requiere `--absorb`. Despachar un agente
*layer* requiere consentimiento en runtime: `--allow-layer` (todos) o
`--allow-layer=nova,legacy` (whitelist), superficie de confianza distinta de
`--allow-workspace`.

---

## 9. Archivo de salida (`.output/<id>.md`)

- **Frontmatter:** `id`, `agent(s)`, `status`, `started`, `finished`, `input`.
- **Cuerpo:** los bloques `[return]` concatenados, más Question / Notices si los
  hubo.
- El contenido con forma de credencial se **redacta** antes de escribir nada.

`.mem/` guarda estado del runtime (checkpoints de resume, run-dirs, cache de
healthcheck) e es inaccesible a los agentes.

---

## 10. Solución de problemas

| Síntoma | Causa / arreglo |
|---|---|
| `arkannie binary not found at .../bin/arkannie` | corre `make build`, o exporta `ARKANNIE_HOME` a la raíz del repo |
| El comportamiento no refleja tus cambios | el shim no recompila — corre `make build` |
| `usage error: ...` (exit 64) | combinación de flags inválida; revisa §3 |
| `VAL-NN: ...` al validar | contrato de agente inválido; ver §5 y `spec/divergence-notes.md` |
| `claude healthcheck failed` | el CLI de `claude` no está en el PATH o `claude_bin` mal configurado |

---

## Referencias
- `spec/ann-lang.md` — especificación normativa del lenguaje Ann.
- `spec/agent-protocol.md`, `spec/agent-schema.yaml` — contrato de agente.
- `spec/divergence-notes.md` — divergencias vs. arkannie + reglas VAL + CLI.
- `arkannie.md` — identidad del runtime (intérprete fallback + Forge).
- `arkannie-absorb.md` — protocolo de absorción completo.
- `TESTING.md` — manual de pruebas pendientes (live/manual).
