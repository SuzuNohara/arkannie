# arkannie — Manual de usuario

**arkannie** es un *harness stateless de agentes de IA*. El binario Go (`arkannie`)
compila y ejecuta programas escritos en **Ann** (un lenguaje de despacho
minimalista), orquestando agentes que corren como procesos `claude` aislados.
El binario es determinista: no hay LLM en el camino de compilación/despacho —
Claude solo ejecuta los agentes.

Versión: `0.3.0` · Lenguaje: Ann v0.3

> La letra dura de la sintaxis y semántica vive en `spec/ann-lang.md` (spec
> **normativa** de Ann v0.3). Este manual es **didáctico**: para cualquier duda
> de comportamiento, la spec y sus tests deciden.

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
tar -xzf arkannie-0.3.0.tar.gz
ln -sf "$PWD/arkannie-0.3.0/bin/arkannie.sh" ~/.local/bin/arkannie
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
| `--man[=agente]` | imprime el manual de ejecución por agente — contrato completo por operación, o el de uno solo (valor solo en forma `=`) |
| `--check <prog.ann>` | valida la sintaxis de un programa sin ejecutarlo (solo parseo; ver §11); exit 0 OK, 1 en error de parseo |
| `validate [--agent=<n>]` | valida los contratos bajo `.agents/` |
| `--version` | imprime la versión de arkannie y sale |
| `--help`, `-h` | imprime el tutorial |

### Códigos de salida
`0` éxito · `1` error · `2` info · `64` error de uso

---

## 4. El lenguaje Ann (v0.3)

Ann es un lenguaje de **despacho**, no de propósito general. Tres niveles
estructurales: **arkannie** (el runtime, Nivel 1), los **wave agents** (Nivel 2,
un proceso claude por dispatch que devuelve un envelope), y los **sub-agents**
(Nivel 3, trabajadores anónimos que un wave crea internamente).

### Estructura del programa
```
# ann v0.3                 // header obligatorio, línea 1, columna 0
// los comentarios son de línea con //
```

### Dispatch — el átomo
```
[seeker] "query" --depth=2 --id=find : contexto verbatim
```
`[command]` es el agente; siguen args posicionales y `--flags`; `--id` etiqueta
el dispatch; todo tras `: ` es contexto verbatim (puede abarcar varias líneas).
Los `$refs` en args o contexto se sustituyen desde RAM.

### Comillas y escapes
Los literales de string van entre `"…"` y reconocen **tres** escapes; el resto es
literal:
```
$q = "con \"comillas\" y \\backslash"   // -> con "comillas" y \backslash
$p = "cuesta \$5 sin interpolar"        // -> cuesta $5   (el $ NO se resuelve)
```
`\"` y `\\` producen la comilla y la barra literales; `\$` produce un `$` literal
y **desactiva** la interpolación de esa posición. Cualquier otro `\X` (p. ej.
`\q`) es error de sintaxis. Los slots `{{ … }}` y las `$ref` reales dentro del
string se transportan verbatim (las `$ref` sí se resuelven). Detalle normativo en
`spec/ann-lang.md §1.4`.

### Contexto multilínea
El bloque de contexto tras `: ` puede abarcar varias líneas. Empieza en la primera
línea indentada y **termina** en la primera línea que sea un dedent, un `}`, o que
contenga `->`. Las líneas en blanco **internas** se conservan (separan párrafos) y
la indentación adicional se preserva relativa:
```
[activity] act-004 :
  intro paragraph

  - item with detail
      nested note
```
Ese contexto llega al agente con su línea en blanco y su sangría anidada intactas.
(En v0.2 la primera línea en blanco cortaba el bloque; ya no.) Ver `§2.7`.

### Bindings (RAM) y `$result`
```
$x = "una cadena literal"
$items = list("a", "b", $x)
$r = [seeker] : encuéntralo      // $r contiene el payload de éxito
```
Cada bloque `{ }` es un scope: los bindings creados dentro desaparecen al cerrar.

### Construir datos: `list`, `concat`, `map`
Ann tiene tres constructores de valores compuestos que anidan libremente entre sí.
Un **elemento** puede ser un literal, una `$ref` (con o sin punto) o un
constructor anidado:
```
$items  = list("a", list("b"), $r.items)      // lista; anida listas
$joined = concat($items, "x")                  // aplana UN nivel, en orden
$cfg    = map(k: "v", n: $r.campo, sub: map(c: "d"))  // mapa clave->valor
```
- `list(...)` crea una lista ordenada e inmutable; una `list()` anidada queda
  anidada.
- `concat(...)` concatena: un argumento que es lista aporta sus elementos, uno que
  no lo es entra suelto en su posición. **Aplana exactamente un nivel** (una lista
  dentro de un argumento sigue anidada). `concat()` es lista vacía.
- `map(k: v, ...)` crea un mapa; las claves son identificadores, los valores usan
  la misma gramática de elemento. Una clave duplicada o mal formada es error de
  sintaxis. Un `map`/`list` que emites con `[return]` sale como bloque YAML.

**Cambio v0.3:** un `$ref` de elemento que no resuelve ya **no** produce un string
vacío silencioso: emite un aviso Class A y ese elemento (o entrada de mapa) se
**omite**; el programa continúa. `concat` y `map` solo son constructores antes de
`(`; como palabra suelta o dentro de texto son literales. Ver `§2.6`.

### Acceso por punto (`$ref.seg.seg`)
Un binding que contiene un mapa se lee campo por campo con puntos. Una ruta con
punto resuelve al **valor** de ese campo — **no** existe un atajo "sobre
completo" (`$r.payload` como el payload entero no aplica):
```
[echo] : el estado es $r.status           // inlinea el valor string
[writer] : la respuesta es $r.payload.out // camina payload, luego out
```
Cada segmento indexa un nivel del mapa por clave. Si un paso intermedio no es un
mapa, o la clave no existe, la ruta no resuelve; en texto de contexto una ruta
irresoluble es un error pre-dispatch (Class B) que nombra base y segmento. La
letra dura está en `spec/ann-lang.md §2.8`.

### Handlers trinarios
Todo agente devuelve `success`, `error` o `info`. Se enganchan hasta tres
handlers; dentro, `$result` expone `{id, status, payload}` — accedidos por punto
al campo concreto que necesitas:
```
[seeker] : busca el config
  success -> { [writer] : usa $result.payload.result }
  error   -> { [notify] no se encontró }
  info    -> { }
```
Un `error` sin handler de error escala y falla la corrida.

### Control de flujo
```
foreach $items { [worker] : $item }      // iteración secuencial sobre una lista
loop limit=3   { [worker] : reintenta }  // repetición acotada (N vueltas exactas)
parallel {                               // despacho concurrente (cada uno --id único)
  [a] --id=one : x
  [b] --id=two : y
}
  each -> { [notify] : $result.payload.out }
```

#### Fan-out dinámico — `parallel foreach`
Cuando el número de despachos depende de una lista de runtime, `parallel foreach`
lanza **una plantilla** de despacho concurrentemente, una vez por elemento:
```
parallel foreach $r.items --id=W {
  [echo] : "$item @ $index"
}
  each -> {
    [notify] : "$result"
  }
```
- `--id=W` es la **base** obligatoria; el runtime sintetiza `W-1`, `W-2`, …
  (1-based). La plantilla **no** lleva `--id` propio.
- Dentro de la plantilla, `$item` es el elemento actual y `$index` su índice
  1-based; solo viven durante el statement.
- Exactamente **una** plantilla por bloque. El `each -> {}` es opcional.
- Aunque corren en paralelo, **el reporte se ensambla en orden de índice** (es
  determinista, no en orden de llegada).
- Lista vacía → cero despachos; un binding que no es lista → aviso Class A y se
  salta. Un error de ítem sin `each` escala (Class B) tras completar todos.

Un `foreach` a solas sigue siendo secuencial; solo `parallel foreach` reparte.
Detalle en `§6.10`.

#### `if` / `else` — condicional determinista
Una guarda elige una rama. Es **una sola** comparación, solo `==` o `!=`, entre
dos operandos; un operando es un `$ref` (con o sin punto), un literal de string,
o `null`. **No** hay operadores compuestos (`&&`, `||`) ni aritmética:
```
if $r.status == "success" {
  [notify] $r.payload.result
}
else {
  [ask-user] reintentar
}
```
`null == null` es verdadero; un `$ref` que no resuelve vale `null`, así que
`$faltante == null` se cumple. Si algún operando resuelve a un **compuesto**
(mapa o lista) la guarda es no comparable: el `if` completo se **salta** (aviso
Class A, no escala) y el programa sigue. La rama elegida corre en su propio
scope.

#### `loop limit=N until <guarda>` — retry hasta éxito
La cláusula `until` opcional convierte el bucle acotado en el patrón canónico de
reintento. La guarda (misma forma que `if`) se evalúa **después** del cuerpo de
cada iteración y **antes** de cerrar su scope, por lo que observa los bindings
que el cuerpo acaba de crear; si se cumple, el loop corta temprano:
```
loop limit=5 until $r.status == "success" {
  $r = [seeker] : poll
}
```
Si el éxito llega en la tercera vuelta, se hacen exactamente tres despachos. Sin
`until`, el loop corre las `N` vueltas completas. Un operando compuesto en la
guarda se trata como no cumplido (Class A): el loop corre hasta `limit`.

### Retry declarativo — `--retry` / `--backoff`
Un solo despacho puede reintentarse por sí mismo con dos flags reservados, sin
escribir un `loop`:
```
[seeker] fetch --id=q --retry=2 --backoff=1
  error -> { [notify] : "agotado tras 3 intentos" }
```
- `--retry=N` autoriza hasta `1 + N` intentos. Solo reintenta un `error`
  **recuperable** (`payload.recoverable: true`) o un **timeout**; un error no
  recuperable no se reintenta.
- `--backoff=S` espera de forma **lineal** antes de cada reintento: `S·1`, `S·2`,
  … segundos. Con `--retry=2 --backoff=2` espera 2 s y luego 4 s.
- Si se agotan los reintentos, el último error sigue siendo **capturable** por un
  `error -> {}` (no es un fallo fatal por sí mismo).
- Solo agentes `agnostic`: `--retry` sobre un `executor` es una parada Class B (no
  se despacha), porque re-ejecutar un executor duplicaría sus efectos.

**¿`--retry` o `loop ... until`?** Usa `--retry` para reintentar **el mismo
despacho** ante un fallo transitorio (timeout, error recuperable), con backoff
gratis. Usa `loop ... until` cuando necesitas **hacer polling** hasta una
condición que tú evalúas (`$r.status == "success"`), reasignando el binding en
cada vuelta, o cuando el cuerpo tiene más de un statement. Ver `§2.10` y `§6.7`.

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

### Composición con `call` — módulos como funciones
`call` ejecuta otro `.ann` como una **función**: RAM aislada y valor de retorno
explícito.
```
$sub = call "sub.ann"    // liga el valor de retorno del módulo
call "sub.ann"           // ejecuta el módulo, sin ligar nada
```
- **RAM aislada:** el módulo no ve los bindings del padre ni los suyos vuelven al
  padre. Es una función pura sobre el sistema de archivos.
- **Valor de retorno:** si el módulo tiene un `[return]` único, `$sub` recibe ese
  valor; si tiene varios `[return]` etiquetados, `$sub` recibe un mapa indexado por
  su `--id`. Los `[return]` del hijo **no** aparecen en el reporte del padre.
- **Profundidad 1:** un módulo invocado no puede a su vez hacer `call` (sin
  recursión). No admite paso de argumentos en v0.3.
- **Seguridad de ruta:** la ruta es relativa al directorio del programa y no puede
  escapar de él; una ruta fuera, un módulo inexistente o una cabecera de versión
  incorrecta detienen la corrida (Class B).
- El frontmatter de la salida lista también los agentes usados por los módulos
  invocados. Detalle en `§2.11`.

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

## 11. Validación local (`--check`)

Antes de correr un programa puedes validar **solo su sintaxis**, sin efectos
secundarios: no carga el registry, no corre el healthcheck de `claude`, no
despacha ningún agente y no escribe `.output/` ni `.mem/`.

```bash
arkannie --check programa.ann
```

- Parseo limpio → imprime una línea `OK` con el descargo **"syntax only — no
  agents were run"** y sale con **exit 0**.
- Error de parseo → lo reporta a stderr como
  `parse error at L:C [categoría]: mensaje` y sale con **exit 1**.

`--check` es mutuamente excluyente con las banderas de ejecución (`--agent`,
`--forge`, `--detach`, `--interpret`) y requiere un input `.ann`; una
combinación inválida es error de uso (**exit 64**). Un `--check` verde garantiza
**solo** que el programa parsea: no valida existencia de agentes, resolubilidad
de bindings ni contratos de operación (ver `spec/ann-lang.md §11`).

---

## Referencias
- `spec/ann-lang.md` — especificación normativa del lenguaje Ann.
- `spec/agent-protocol.md`, `spec/agent-schema.yaml` — contrato de agente.
- `spec/divergence-notes.md` — divergencias vs. arkannie + reglas VAL + CLI.
- `arkannie.md` — identidad del runtime (intérprete fallback + Forge).
- `arkannie-absorb.md` — protocolo de absorción completo.
- `TESTING.md` — manual de pruebas pendientes (live/manual).
