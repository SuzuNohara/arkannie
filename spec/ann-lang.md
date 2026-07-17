# Especificación del lenguaje Ann v0.3

## Autoridad del documento

Este archivo es la especificación **normativa y nativa** del lenguaje Ann tal como lo
implementa el runtime `arkannie` (binario Go). Describe el comportamiento **ya
implementado y probado**: el código de `internal/ann`, `internal/ram`,
`internal/dispatch`, `internal/scheduler` y `cmd/arkannie`, junto con sus tests, es la
fuente de verdad. Ante cualquier duda de semántica, el test correspondiente decide (ver
el apéndice de trazabilidad al final). Este documento prevalece sobre cualquier otra
descripción de sintaxis o semántica de Ann en el repositorio, con una excepción: la
mecánica del sobre de retorno (envelope), los timeouts y la herencia de grants viven en
`agent-protocol.md`, que es normativo para esos temas; aquí solo se referencian.

Ann es un **lenguaje de despacho**, no un lenguaje de propósito general. Un programa Ann
orquesta agentes wave (cada uno un proceso `claude` aislado), pasa resultados por
bindings de RAM y decide explícitamente qué va a la salida. Su gramática es
deliberadamente pequeña.

---

## Preámbulo — no-objetivos (léase primero)

Esta versión especifica **únicamente** lo que el runtime hace hoy. Las siguientes
capacidades **no** son parte de Ann v0.3 y **no deben** inferirse de este documento; son
material de v0.4 o posterior:

- **Sin operadores compuestos en guardas.** Las guardas de `if` y de `loop ... until`
  admiten **solo** `==` y `!=`. No existen `&&`, `||`, negación, paréntesis ni
  aritmética. Una sola comparación por guarda.
- **Comparación solo de strings y `null`.** Un operando que resuelve a un valor
  compuesto (mapa o lista) **no es comparable**: la guarda se trata como no evaluable
  (Class A, ver §6.9 y §6.7). No hay comparación estructural, numérica ni de longitud.
- **Los juicios semánticos pertenecen a los agentes.** El runtime no interpreta el
  contenido de un payload ni "entiende" texto: compara valores literales. Cualquier
  decisión que requiera comprender lenguaje natural debe delegarse a un agente wave, que
  devuelve un campo escalar sobre el que la guarda pueda comparar.
- **Sin funciones de usuario (UDF).** Ann no define funciones propias más allá de los
  constructores nativos (`list`, `concat`, `map`, `call`) y las palabras clave compiladas.
  No hay declaración de procedimientos ni operadores definidos por el usuario.
- **`call` sin argumentos.** Un módulo se invoca con `call "ruta.ann"` y recibe RAM vacía
  (§2.11): **no** admite paso de parámetros. El paso de argumentos a `call` es material de
  v0.4.
- **Sin recursión de `call`.** La profundidad de `call` está fijada en 1: un módulo
  invocado **no** puede a su vez invocar `call` (§2.11). No hay recursión ni cadenas de
  invocación.
- **Determinismo no negociable.** La clasificación de comandos, la resolución de
  bindings, la evaluación de guardas, el ruteo de handlers y el ensamblado del reporte de
  un fan-out (por índice, §6.10) son deterministas y sin estado oculto. Ninguna
  construcción de v0.3 introduce no-determinismo observable.
- **Composición limitada de bloques.** `parallel {}` sigue sin admitir anidamiento
  (§6.1) y `parallel foreach` admite **exactamente una** plantilla de despacho (§6.10). La
  composición/anidamiento arbitrario de bloques de control sigue fuera de alcance.
- **Plantillas de usuario sin resolver.** Las plantillas `{{ ... }}` dentro del texto de
  usuario no se resuelven; se transportan verbatim (ver §5).

> **Novedades v0.3** (ya implementadas y normadas aquí, retiradas de no-objetivos respecto
> de v0.2): sistema de comillas y escapes (§1.4), semántica multilínea del bloque de
> contexto (§2.7), constructores de datos `concat()` y `map()` (§2.6), fan-out dinámico
> `parallel foreach` (§6.10), reintento declarativo `--retry`/`--backoff` (§2.10) y
> composición de módulos `call` (§2.11).

---

## Arquitectura de niveles

Tres niveles. Cada uno es un contrato, no una descripción.

| Nivel | Identidad | Ciclo de vida | Superficie de protocolo |
|---|---|---|---|
| **Nivel 1 — arkannie** | El runtime. Binario Go que compila e interpreta Ann de forma determinista. Único invocador de agentes. | Permanente durante toda la ejecución del programa. | Ninguna — arkannie nunca es receptor de un envelope. |
| **Nivel 2 — agentes wave** | Agentes efímeros despachados como procesos `claude -p` aislados. Definidos por `.agents/<nombre>/agent.yaml` + `harness.md`. | Se spawnean por despacho, se destruyen al retornar. | Devuelven exactamente un envelope `{ id, status, payload }` (ver `agent-protocol.md`). |
| **Nivel 3 — sub-agentes** | Trabajadores anónimos construidos en línea por un Nivel 2. Sin archivos. | Se spawnean dentro de un wave, invisibles a arkannie. | Devuelven su payload solo al Nivel 2 padre. |

**Invariante:** Nivel 1 es arkannie, Nivel 2 es un wave, Nivel 3 es un sub-trabajador.
No son roles, son posiciones estructurales; un agente no cambia de nivel durante una
ejecución.

La superficie de ejecución es **batch**: se invoca con `arkannie ... programa.ann` o
`arkannie --agent <nombre> ... "prompt"`. La única superficie conversacional vive en
`--forge` (forja de agentes) y en `--interpret` (reparación de un programa ante error de
parseo). No hay modo interactivo persistente.

---

## §1 Estructura léxica

### §1.0 Cabecera de versión

Todo programa Ann (`.ann`) **debe** comenzar con la cabecera, en la primera línea no
comentario:

```
# ann v0.3
```

El `#` **debe** ser el primer carácter de la línea (columna 0). Un valor distinto de
`# ann v0.3` es un error de parseo de categoría *Version mismatch* (Class B) — incluida la
cabecera heredada `# ann v0.2`, que ahora se **rechaza**. En modo prompt (interactivo
contra un solo agente) la cabecera es opcional y se ignora si está presente.

### §1.1 Comentarios

```
// esto es un comentario
```

Los comentarios `//` son de línea únicamente. No hay comentarios de bloque. Pueden
aparecer en cualquier posición donde sea válido un salto de línea.

### §1.2 Símbolos reservados

| Símbolo | Rol |
|---|---|
| `[nombre]` | Token de comando |
| `{{ clave }}` | Slot de plantilla — resuelto solo al renderizar el `harness.md` (ver §5) |
| `$nombre` | Referencia a un binding de RAM; admite acceso por punto `$nombre.seg` (ver §2.8) |
| `->` | Flecha de handler |
| `{}` | Delimitadores de bloque |
| `//` | Marcador de comentario |
| `#` | Marcador de cabecera de versión (solo línea 1) |
| `--` | Prefijo de flag |
| `==` `!=` | Operadores de comparación de guarda (ver §6.7, §6.9) |
| `:` | Separador del bloque de contexto (ver §2.7) |

### §1.3 Palabras clave del lenguaje

Los siguientes tokens tienen semántica fija en la gramática de Ann. **No pueden** usarse
como nombre de binding:

```
parallel  foreach  loop  success  error  info  each  limit
ask-user  notify  clarify  null  return
```

Además, la gramática recontextualiza estructuralmente estas palabras:

- `if` y `else` introducen el condicional determinista (§6.9). Un `[if]` con corchetes o
  un `while`/`[while]` son formas **rechazadas** con error de sintaxis (usar `if` o
  `loop`, respectivamente).
- `until` es palabra reservada **solo** en la posición de cabecera de un `loop`
  (`loop limit=N until <guarda> {`, ver §6.7). En cualquier otra posición `until` es
  texto libre.
- `parallel foreach` es una forma de dos palabras que introduce el fan-out dinámico
  (§6.10). Un `foreach` a solas sigue siendo iteración secuencial (§6.6); solo la
  secuencia `parallel foreach` dispara el fan-out.

**Palabras clave sensibles a la posición** (`concat`, `map`, `call`): son constructores
**solo** cuando aparecen en posición de expresión inmediatamente seguidas de su
delimitador de apertura — `concat(`, `map(` y `call "` (`call` seguido de un literal de
string). En cualquier otra posición (argumento posicional suelto, texto de contexto,
interior de un literal de string) son **texto libre** y nunca rompen el parseo. Esta regla
es normativa: `[echo] use map for config` produce cuatro argumentos de texto, no un
constructor.

**Referencias implícitas del fan-out** (`$item`, `$index`): dentro de la plantilla de un
`parallel foreach` (§6.10) el runtime liga `$item` al elemento actual y `$index` a su
índice **1-based**. Fuera de esa plantilla no existen; referirlas es un aviso Class A.

---

### §1.4 Literales de string, comillas y escapes

Un literal de string se delimita con comillas dobles `"…"`. Dentro de un literal se
reconocen **exactamente tres** secuencias de escape; el resto de caracteres es literal.

| Secuencia | Resultado | Momento de resolución |
|---|---|---|
| `\"` | una comilla doble literal `"` | léxico (el lexer produce el string ya des-escapado) |
| `\\` | una barra invertida literal `\` | léxico |
| `\$` | se **conserva verbatim** (`\$`) en el token, y luego se convierte en un `$` literal en la pasada de interpolación | interpolación (§2.8) |

- Cualquier otra secuencia `\X` (por ejemplo `\q`, `\n`) es un **error léxico** de
  categoría *Syntax* (Class B), reportado en la columna de la barra invertida.
- Un literal sin cerrar (`"abierto` sin comilla final) es un error léxico *Syntax*.
- Los tokens que **no** son escapes se transportan verbatim dentro del literal: `{{ slot }}`,
  `$ref` (que sí se interpola, §2.5), `//` y el resto de puntuación.

**El escape `\$` — mecanismo de una pasada.** `\$` no es un escape léxico: el lexer y el
parser lo llevan intacto (`\$`) hasta las posiciones donde se interpola (argumentos de
despacho, literales de string, elementos de `list()`, texto de contexto — §2.8). Allí una
pasada única enmascara cada `\$` para que el patrón de referencia **no** lo tome como una
`$ref`, resuelve las referencias reales, y finalmente restaura la máscara como un `$`
literal. Consecuencias normativas:

- `"price \$5"` produce el texto `price $5`; el `$5` **no** se resuelve contra RAM.
- Un `\$nombre` de un binding inexistente **no** es un error de ruta irresoluble (§7.3): el
  `$` es literal, no hay referencia que resolver.
- Una `$ref` real en el mismo texto sigue resolviéndose con normalidad.

---

## §2 Gramática de tokens

### §2.1 Átomo de comando

```
[comando] arg1 arg2 --flag1 --flag2=valor
```

- El nombre del comando es `[palabra]` — alfanumérico más `-`, sin espacios dentro de los
  corchetes.
- Los argumentos son strings posicionales (sin comillas para valores de una palabra).
- Los flags llevan prefijo `--`; los flags booleanos no tienen valor, los flags con valor
  usan `=`.
- **Flags reservados del runtime.** `--id`, `--timeout`, `--retry` y `--backoff` son
  directivas built-in que el runtime consume: **no** se declaran en la operación del agente
  ni se transportan en el `context_block` (§9). El resto de los flags deben estar
  declarados por la operación (§9). `--retry`/`--backoff` se especifican en §2.10.
- Un átomo de comando en su propia línea es un statement completo.
- Un átomo de comando seguido de handlers `->` es un despacho con ruteo de resultado.

### §2.2 Handlers trinarios

Todo despacho a un wave puede ir seguido, opcionalmente, de handlers trinarios:

```
[comando] args
  success -> { ... }
  error   -> { ... }
  info    -> { ... }
```

- Los tres handlers son opcionales.
- Un handler se ejecuta cuando el wave retorna con el `status` que coincide.
- Dentro de un handler, `$result` expone `{ id, status, payload }` del wave.
- Si un handler está ausente y el wave retorna ese status:
  - `success` sin handler → el payload **no** se vuelca a la salida; queda solo en RAM.
  - `error` sin handler → escalación Class B (ver §7).
  - `info` sin handler → se descarta, **salvo** que `payload.missing_field` esté presente
    (Ask Protocol, §2.7.1), en cuyo caso arkannie siempre surface el mensaje.
- Los cuerpos de handler son bloques Ann; se ejecutan en un scope propio con `$result`
  ligado (ver §4).

El sobre de retorno, su validación estructural y el ruteo por `status` están normados en
`agent-protocol.md §1–§2`; aquí no se re-especifican.

### §2.3 Asignación de binding

```
$nombre = [comando] args
$nombre = "cadena literal"
$nombre = list("a", "b", "c")
$nombre = concat($a, $b)
$nombre = map(clave: "valor")
$nombre = call "modulo.ann"
```

- Los bindings son locales a RAM (ver §4).
- El lado izquierdo es `$identificador` — alfanumérico más `_`, sin `-`; no puede ser una
  palabra reservada de §1.3.
- El lado derecho es una **expresión**: un átomo de comando, un literal de string, un
  constructor de datos (`list()`, `concat()`, `map()`, §2.6) o una invocación de módulo
  `call "…"` (§2.11).
- Una asignación no produce salida; el resultado se guarda solo en RAM.
- Si el comando retorna `error`, el binding **no** se liga y aplican las reglas de
  escalación de error. Si el comando retorna `success`, el binding recibe el `payload`.

### §2.4 Panorama de control de flujo

Las construcciones de control se detallan en §6. Formas admitidas:

- `parallel { ... } [each -> { ... }]` — despacho concurrente estático (§6.1–§6.5, §6.8).
- `parallel foreach $lista --id=BASE { plantilla } [each -> { ... }]` — fan-out dinámico
  sobre una lista (§6.10).
- `foreach $lista { ... }` — iteración secuencial (§6.6).
- `loop limit=N [until <guarda>] { ... }` — repetición acotada con post-condición
  opcional (§6.7).
- `if <guarda> { ... } [else { ... }]` — condicional determinista (§6.9).

### §2.5 String interpolado

```
"texto con referencias $binding y $binding.campo"
```

- `$binding` — resuelto desde RAM en tiempo de ejecución (§2.8).
- Un `\$` escapa la interpolación: produce un `$` literal y **no** se resuelve (§1.4).
- Los slots `{{ clave }}` que aparezcan dentro del texto de usuario **no** se resuelven:
  se transportan verbatim (ver §5). La única sustitución dentro de texto de usuario es
  `$ref`/acceso por punto (con `\$` como su escape).

### §2.6 Constructores de datos (`list()`, `concat()`, `map()`)

Ann construye valores compuestos con tres constructores nativos. Todos comparten la misma
gramática de **elemento**, de modo que anidan libremente entre sí.

**Elemento (gramática común).** Un elemento es exactamente uno de:

- un literal de string (`"…"`, con las comillas y escapes de §1.4);
- una referencia `$ref` (incluida una ruta con punto, §2.8; la ruta se conserva sin el `$`);
- un constructor anidado `list(...)` o `map(...)`.

**Resolución de un `$ref` de elemento (cambio respecto de v0.2).** Un `$ref` de elemento
que **no** resuelve **ya no** se sustituye por un string vacío: emite un aviso Class A que
nombra el binding y el elemento se **omite** del valor construido; el programa continúa. La
misma regla aplica a `list()`, `concat()` y a los valores de `map()`.

#### `list()` — lista ordenada

```
$items = list("alpha", "beta", "gamma")
$items = list($a, $b, $c)
$anidada = list("a", list("b"), $r.items)
```

- Crea un binding de tipo lista (`KList`).
- Un elemento `list(...)` anidado produce una lista anidada (no se aplana).
- Las listas son inmutables tras su creación.

#### `concat()` — concatenación con aplanado de un nivel

```
$joined = concat($items, "x")
$joined = concat($lista, $otra, $cola)
```

- Toma cero o más argumentos, cada uno un **elemento** (misma gramática).
- **Aplana exactamente un nivel**, en orden estable de izquierda a derecha: un argumento
  que resuelve a lista aporta sus elementos directos; un argumento que **no** es lista
  aporta un solo elemento suelto en su posición. Una lista **anidada dentro** de un
  argumento permanece anidada (solo se aplana el nivel superior).
- `concat()` sin argumentos es una lista vacía válida; `concat($a)` con un único argumento
  lista es una copia de esa lista.
- Un argumento `$ref` irresoluble se omite con aviso Class A (ver arriba).

#### `map()` — mapa ordenado clave→valor

```
$cfg = map(k: "v", n: $r.campo)
$cfg = map(a: list("x"), b: map(c: "d"))
```

- Crea un binding de tipo mapa (`KMap`).
- Cada entrada es `clave: valor`. La **clave** es un identificador (`[A-Za-z0-9_]+`); una
  clave entre comillas o no-identificador es un error *Syntax* con mensaje `map key`. El
  **valor** es un elemento (la misma gramática de §2.6, incluidos `list()`/`map()`
  anidados y rutas con punto).
- El `:` es un separador **solo dentro del constructor `map(...)`**; fuera de él conserva
  su papel de separador del bloque de contexto (§2.7). El lexer distingue ambos por
  profundidad de paréntesis.
- Una **clave duplicada** es un error *Syntax* que nombra la clave, con su `L:C`.
- Formas malformadas — falta el `:`, paréntesis sin cerrar, clave no-identificador, valor
  vacío — son errores *Syntax* con mensaje específico de `map` y un `L:C` válido.
- Un valor `$ref` irresoluble omite la entrada con aviso Class A.
- Un `map()` puede aparecer como elemento dentro de `list()` y `concat()`.
- Un `KMap` emitido por `[return]` se renderiza como bloque YAML con cerca (```` ```yaml ````).

**`concat` y `map` como texto.** Ambos nombres son constructores **solo** inmediatamente
antes de `(` en posición de expresión (§1.3). Como argumento posicional suelto, dentro del
texto de contexto o dentro de un literal de string, son texto verbatim.

### §2.7 Bloque de contexto

Un átomo de comando puede ir seguido de un bloque de contexto: texto libre que el agente
wave interpreta para extraer la información que necesita.

```
[comando] arg1 --flag1 : el texto de contexto va aquí
```

El `: ` (dos puntos + espacio) separa la cabecera estructurada del bloque de contexto.

- **Una línea:** el contexto termina al fin de línea.
- **Multilínea (semántica v0.3, reemplaza la regla de corte por línea en blanco):** el
  bloque comienza en la primera línea siguiente si está indentada, y continúa capturando
  las líneas subsiguientes. El bloque **termina** en la primera línea que cumpla una de:
  (a) un **dedent** — una línea menos indentada que la primera línea del bloque; (b) una
  línea `}`; o (c) una línea que contenga el token de handler `->`.
- **Líneas en blanco internas preservadas.** Una línea en blanco **dentro** del bloque se
  conserva (es parte del texto: separa párrafos). Las líneas en blanco **finales** se
  descartan (son separadores, no contenido). Esto invierte la regla de v0.2, donde la
  primera línea en blanco cortaba el bloque.
- **Indentación relativa preservada.** Solo se recorta el prefijo común (la indentación de
  la primera línea del bloque); la indentación adicional de las líneas más profundas se
  conserva relativa. Así, listas y notas anidadas dentro del contexto mantienen su forma.
- **Primera línea no indentada:** no hay bloque de contexto (contexto vacío).
- **Sin contexto:** las operaciones que no lo necesitan omiten el `:` por completo.

**Mapeo al `context_block`:** arkannie coloca el texto en `context_block.context.text` y
resuelve antes del despacho los `$ref` que contenga (§2.8). arkannie no parsea ni valida
el contenido del texto — eso es responsabilidad del agente. El `context_block` canónico se
detalla en §9.

**Responsabilidad de extracción del agente:** el wave recibe `context.text` y debe extraer
los campos que necesita. Si un campo requerido no puede determinarse, el agente devuelve
`status: info` con una pregunta (§2.7.1) en lugar de proceder con datos faltantes.

### §2.7.1 Ask Protocol del agente

Cuando un wave no puede determinar un campo requerido, retorna `status: info` con una
pregunta en vez de `status: error`:

```yaml
id: "..."
status: info
payload:
  message: "¿Cuál es el tipo de actividad? (simple | project)"
  missing_field: "type"
  resumable: true
```

**Comportamiento de arkannie ante `info` con `missing_field`:** arkannie siempre surface el
`message` al desarrollador — no lo descarta en silencio (excepción a la regla de descarte
de `info` de §2.2) — y marca el status del programa como `info`. El desarrollador
re-emite el comando con la información añadida al contexto y arkannie re-despacha. No hay
re-despacho automático.

`resumable: true` indica que el agente espera ser re-despachado; `resumable: false` (o
ausente) significa que el agente se rindió: `info` terminal, sin re-despacho esperado.

### §2.8 Referencias con acceso por punto

**Gramática:**

```
$nombre(.segmento)*
```

`nombre` y cada `segmento` son `[A-Za-z0-9_]+`. `$x` es la forma sin punto; `$x.a.b` es
una ruta de acceso por punto. El token canónico es
`\$[A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)*` (definido una sola vez en `ram.RefToken` y
consumido en todos los sitios). El lexer parte `$x.a.b` en `[$x, .a, .b]` y cada sitio que
admite referencia los vuelve a unir en una sola ruta.

**Semántica de `Resolve` sobre KMap:** el primer segmento se resuelve como una lectura de
binding normal (recorriendo scopes de adentro hacia afuera, §4). Cada segmento adicional
indexa dentro de un valor de tipo mapa (KMap) por su clave. Si un paso intermedio no es un
mapa, o la clave no existe, la ruta **no resuelve**. La forma sin punto es exactamente una
lectura de binding.

**Sitios donde se preserva la ruta con punto:** argumentos de despacho (incluido
`[return]`), lista de `foreach`, elementos de `list()`, operandos de guarda (`if` /
`loop ... until`) y texto de contexto interpolado.

**Resolución dependiente de la posición** (esto es normativo):

- **En interpolación** (texto de contexto y `$ref` dentro de texto): un `$x.campo`
  interpola el **valor** del campo, no el mapa completo ni el token literal. Una ruta
  profunda camina mapas anidados. Un `$ref` **sin punto** a un mapa/lista se agrega como
  campo `context.<último-segmento>`; un `$ref` a un string se inlinea como valor. Una ruta
  que **no resuelve** en esta posición es **Class B** antes del despacho: el error nombra
  el binding base y el segmento que falló. Si la ruta intenta descender en un valor que no
  es mapa, el error **sugiere separar el punto de la referencia** (el punto probablemente
  era texto literal).
- **En guardas** (`if` / `until`): un `$ref` (con o sin punto) que **no resuelve** vale
  `null`. Un `$ref` que resuelve a un valor compuesto (mapa o lista) hace la guarda **no
  comparable** → Class A (§6.9, §6.7).
- **En `[return]`:** resuelve el valor (campo o binding completo); un binding no ligado es
  un aviso Class A y se salta el `[return]` (ver §6.9 no aplica — ver la definición de
  `[return]` en §2.9).
- **En `foreach`:** la ruta debe resolver a una lista; en caso contrario, aviso Class A y
  se salta el `foreach`.

### §2.9 Palabras clave nativas

Cuatro comandos están compilados en el binario y el runtime los ejecuta directamente sin
despachar un wave:

- `[ask-user] <texto>` — surface una pregunta; la ejecución se detiene con status `info`.
- `[notify] <texto>` — añade una nota a la sección *Notices* del reporte.
- `[clarify] <texto>` — igual que `notify`, para aclaraciones.
- `[return] <operando>` — emite un bloque de salida (el indicador de salida, ver abajo).

**`[return]` — indicador de salida (normativo).** El programa decide qué aparece en la
salida. Los payloads de `success` **no** se vuelcan automáticamente: hay que ligarlos y
emitirlos explícitamente con `[return]`.

```
[return] $summary               // return único: sin encabezado, solo el contenido
[return] --id=result $summary   // sección titulada "## result"
[return] "una nota fija"        // literal de string, verbatim
```

Reglas del operando:

- Un `[return]` toma **un** operando: un `$binding` (resuelto por `Resolve`, admite punto)
  o un literal de string.
- Un binding que resuelve a mapa o lista se renderiza como bloque YAML; un string se
  renderiza verbatim.
- Un binding no ligado es un aviso Class A y se salta ese `[return]`. Un `[return]` sin
  operando es un aviso Class A y se salta.
- Un programa sin ningún `[return]` produce un cuerpo de salida vacío.

Reglas de etiqueta de sección, verificadas en tiempo de parseo (violarlas es error de
compilación):

- El `--id` de `[return]` es la **etiqueta de sección** de la salida (distinta del `--id`
  de CLI que nombra el archivo de salida).
- Un único `[return]` puede omitir `--id`: su sección no lleva encabezado.
- Con dos o más `[return]`, **cada uno** debe llevar `--id`.
- Todos los valores de `--id` deben ser únicos.
- Un `[return]` dentro de un `foreach`/`loop`/`each` requiere `--id`; cada corrida emite su
  propia sección numerada (`--id-1`, `--id-2`, …).

### §2.10 Reintento declarativo — `--retry` / `--backoff`

Un despacho puede declarar un reintento con dos flags reservados del runtime (§2.1); ambos
son consumidos por arkannie y **no** llegan al agente.

```
[seeker] retryable --retry=2 --backoff=1
```

- `--retry=N` autoriza hasta **`1 + N`** intentos completos del mismo despacho. El valor
  por defecto es `0`: sin `--retry`, un despacho hace exactamente un intento (semántica
  idéntica a v0.2).
- **Solo se reintenta un resultado reintentable:** un envelope de `error` con
  `payload.recoverable: true`, o el envelope de `error` sintetizado a partir de un
  **timeout** de la invocación. Un `error` **no recuperable** (`recoverable: false`) no se
  reintenta: un solo intento y luego la escalación normal de error no manejado (§7). Los
  status `success` e `info` nunca se reintentan.
- **Reintentos agotados = capturables.** Cuando se agotan los `N` reintentos, el último
  envelope de `error` sigue siendo enrutable por un handler `error -> {}`; **no** es una
  escalación Class B fatal por sí misma. Sin handler de error aplica la escalación normal.
- El **retry correctivo interno** (la re-invocación por violación de protocolo del envelope,
  R10) es independiente: ocurre dentro de **un** intento declarativo y **no** consume un
  `--retry`.
- `--backoff=S` introduce una espera **lineal** antes de cada reintento: antes del reintento
  `n` (1-based) se pausa `S * n` segundos. Con `--retry=2 --backoff=2`, las esperas son
  `2 s` y luego `4 s`. Sin `--backoff` no hay espera.
- **Candado del executor.** `--retry` sobre un agente de scope `executor` es una parada
  **Class B pre-despacho**: **nada** se despacha. Re-ejecutar un executor duplicaría sus
  efectos secundarios; el retry declarativo se restringe a agentes `agnostic`.
- Un `--retry`/`--backoff` negativo es un error de tipo Class A.

### §2.11 `call` — composición de módulos

`call` invoca otro programa `.ann` como una **función**: RAM aislada, valor de retorno
explícito, sin fugas de estado en ninguna dirección.

```
call "sub.ann"                 // statement: ejecuta el módulo, no liga nada
$sub = call "sub.ann"          // expresión: liga el valor de retorno del módulo
```

**Gramática.** `call` es una palabra clave sensible a la posición (§1.3): es `call`
**solo** seguido inmediatamente de un **literal de string** en posición de statement o de
expresión de asignación. `call` sin ruta, `call` con una ruta que no es literal de string
(`call mod.ann`), o `$x = call` son errores *Syntax* reportados en el `L:C` de la palabra
clave. Fuera de esas dos posiciones, `call` es texto libre.

**Semántica de función:**

- **RAM aislada.** El módulo hijo arranca con RAM vacía: los bindings del padre son
  invisibles al hijo, y los bindings del hijo nunca se filtran de vuelta al padre.
- **Profundidad 1.** El hijo **no** puede a su vez `call`: un `call` anidado es una parada
  Class B cuyo detalle menciona la profundidad (`depth`). No hay recursión (no-objetivo).
- **Checkpoint apagado en el hijo.** El protocolo de checkpoint (§10) no opera dentro del
  módulo invocado.
- **Valor de retorno.** El valor que `call` liga depende de los `[return]` del hijo: un
  único `[return]` liga ese valor; dos o más `[return]` etiquetados ligan un `KMap`
  indexado por su `--id`. Un `call` en forma de statement (sin asignación) ejecuta el
  módulo pero no liga nada; referir el binding inexistente es un aviso Class A.
- **Aislamiento de la salida.** Los `[return]` del hijo **nunca** aparecen en el reporte
  del padre; solo alimentan el valor de retorno.

**Seguridad de ruta.** La ruta se resuelve relativa al directorio del programa padre, se
normaliza (`Clean`) y **debe** quedar bajo ese directorio (comprobación de prefijo). Una
ruta que escapa del directorio (`"../fuera.ann"`) es una parada Class B.

**Carga y errores.** Un módulo inexistente y una cabecera de versión incorrecta son ambos
Class B, con la línea del sitio de `call` en el detalle. Un hijo que falla (p. ej. un error
no manejado dentro del módulo) escala Class B en el padre; el checkpoint del padre **no**
registra el `call` como paso completado, de modo que un resume re-ejecuta el `call`
completo.

**Directorios de corrida y frontmatter.** Las corridas del hijo viven bajo
`<runID>/call-<n>/` en `.mem`. El frontmatter de la salida del padre (`agent(s)`) **une**
los agentes despachados por los módulos invocados (parseados a profundidad 1, relativos al
directorio del programa padre) con los del propio padre.

---

## §3 Clasificador de instrucciones

La clasificación es determinista. Para cada statement, arkannie decide en este orden y
toma la primera coincidencia:

```
1. Palabra clave de control de flujo (parallel, parallel foreach, foreach, loop, if) → se maneja localmente
2. Palabra clave nativa (ask-user, notify, clarify, return) o composición (call)     → la ejecuta el runtime
3. [comando] resuelto contra el registry de agentes (.agents/)                        → despacho wave (proceso claude)
4. Si no resuelve → escalación Class B: comando desconocido
```

No existe el comando `[mem]` ni `[personality]` como comando: `.mem/` es memoria exclusiva
del runtime (checkpoints §10, directorios de corrida, caché de healthcheck), inaccesible a
los agentes; las personalities son una capa de render (campo `personality:` en
`agent.yaml`).

### §3.1 Descubrimiento del registry de agentes

Al arranque, arkannie escanea el directorio `.agents/` y construye el registry:

1. Cada subdirectorio `.agents/<nombre>/` con un `agent.yaml` válido registra `<nombre>`
   como comando wave. El contrato del agente (`agent.yaml`) más su plantilla `harness.md`
   definen el agente; arkannie entrega al proceso claude el prompt renderizado completo.
2. `agent.yaml` se valida al arranque (reglas VAL: `model ∈ {haiku,sonnet,opus}`,
   `scope ∈ {agnostic,executor}`, `grants` subconjunto permitido según `scope`,
   `capabilities` con `purpose`+`use_when`, y `layer.origin` válido cuando aplique). Un
   agente que falla la validación se **excluye** solo a sí mismo; el resto carga normal.
3. Las palabras clave nativas y de control de flujo están siempre disponibles,
   independientemente del escaneo.
4. El único escritor de `.agents/` es el Agent Forge (`arkannie --forge`).

---

## §4 Reglas de scope

### §4.1 Qué es un bloque

Un bloque es cualquier cuerpo delimitado por `{}`: cuerpos de handler, cuerpos de
`parallel`, `foreach`, `loop` y ramas de `if`. El bloque es la unidad de scope.

### §4.2 Visibilidad de bindings

RAM es una pila de scopes: cada bloque `{}` hace `Push` de un scope nuevo y, al salir,
`Pop` que destruye sus bindings.

- Los bindings creados en un bloque externo son visibles en los bloques internos.
- Los bindings creados en un bloque interno **no** son visibles en los externos.
- Los bindings creados en un sub-bloque `parallel` **no** son visibles a sus hermanos.
- Los bindings creados en `each ->` viven solo para esa ejecución del handler.
- La resolución de un nombre recorre los scopes de adentro hacia afuera; los internos
  sombrean a los externos.

### §4.3 Vida de RAM

**Modo prompt (interactivo contra un agente):** RAM persiste durante el turno; se limpia
en el límite de turno.

**Modo programa (`.ann`):** RAM persiste durante toda la ejecución del programa y se limpia
al terminar (éxito o error). Aplica el protocolo de checkpoint de §10.

---

## §5 Motor de plantillas

El motor de plantillas de arkannie opera sobre el `harness.md` del agente en tiempo de
render, rellenando **cuatro slots provistos por el runtime**:

```
{{ context_block }}     el context_block canónico serializado (§9)
{{ id }}                el id del despacho
{{ directives_pre }}    bloque de directivas antes del contexto (grupos + personality)
{{ directives_post }}   bloque de directivas después del contexto (modifiers)
```

Los slots `{{ clave }}` que aparezcan **dentro del texto de usuario** (contexto de un
despacho o literales de string) **no** se resuelven: se transportan verbatim. La única
sustitución dentro de texto de usuario es `$ref`/acceso por punto (§2.8), con `\$` como su
escape (§1.4). Un motor de slots de usuario con condicionales (`{{#if}}`) y fallbacks
(`{{ clave | ... }}`) **no** es parte de Ann v0.3 (no-objetivo).

---

## §6 Semántica de control de flujo

### §6.1 `parallel` — `--id` requerido

Todo despacho dentro de un bloque `parallel {}` **debe** llevar `--id=<identificador>`. El
id se usa para correlación (ver `agent-protocol.md §3`). Un `--id` ausente es error de
parseo; un `--id` duplicado dentro del mismo bloque también.

```
parallel {
  [seeker] --id=seek-a alpha
  [reviewer] --id=rev-b beta : parallel context
}
  each -> {
    [notify] $result
  }
```

Los despachos corren concurrentemente. `parallel {}` no admite anidamiento: un `parallel`
dentro de otro es error de sintaxis. Solo se admiten átomos de despacho dentro del bloque.

### §6.2 Ejecución del handler `each`

El handler `each ->` se llama una vez por despacho completado. arkannie expone
`$result.id`, `$result.status` y `$result.payload`. El cuerpo se ejecuta en serie, en orden
de completado; arkannie no re-entra al handler hasta que la ejecución actual termina.

### §6.3 Regla de completado

`parallel {}` está completo cuando todos los waves despachados retornaron (cualquier
status). arkannie procede entonces al siguiente statement después del bloque.

### §6.4 Status `info` en `parallel`

Un wave que retorna `info` dentro de `parallel {}` se trata como notificación no terminal:
el wave se considera completo y su resultado se pasa a `each ->` con `status: info`. El
bloque sigue esperando a los despachos restantes.

### §6.5 Salida de `parallel`

El bloque `parallel {}` no produce binding. A los resultados se accede solo por el handler
`each ->`.

### §6.6 `foreach`

```
foreach $items {
  [seeker] $item
}
```

Iteración **secuencial**. `$item` se liga automáticamente al elemento actual en cada
vuelta. El cuerpo se ejecuta una vez por elemento. La lista puede provenir de una ruta con
punto (`foreach $r.items { ... }`). Un `foreach` sobre una lista vacía es un no-op. Si el
binding **no** resuelve a una lista, es un error de tipo en runtime: aviso Class A y se
salta (§7.3).

### §6.7 `loop limit=N [until <guarda>]`

```
loop limit=5 until $r.status == "success" {
  $r = [seeker] poll
}
```

- Ejecuta el cuerpo hasta `N` veces. `N` **debe** ser un entero positivo; `N` no entero o
  `N ≤ 0` es un error de tipo, Class A, en tiempo de parseo.
- La cláusula `until <guarda>` es opcional y va entre el `limit` y el `{`. La guarda es una
  comparación determinista `operando (==|!=) operando` (misma forma que `if`, ver §6.9).
- **Post-condición TRAS el cuerpo y ANTES del `Pop`.** La guarda `until` se evalúa después
  de ejecutar el cuerpo de cada iteración y **antes** de destruir el scope de esa
  iteración, de modo que **observa los bindings que el cuerpo acaba de crear**. Si la
  guarda se cumple, el loop se rompe temprano.
- **Retry-until-success es el patrón canónico:** asignar dentro del cuerpo
  (`$r = [agente] ...`) y cortar cuando `$r.status == "success"`. En el ejemplo, si el
  éxito llega en la tercera vuelta, el loop hace exactamente tres despachos.
- **Sin `until`:** el loop corre exactamente `N` iteraciones (semántica de repetición
  acotada previa).
- Un operando de la guarda que resuelve a un valor compuesto (mapa/lista) es **no
  comparable**: aviso Class A tratado como **no cumplido**, por lo que el loop corre hasta
  `limit` y el programa continúa (no escala).

### §6.8 Escalación de error dentro de `parallel`

Si un despacho dentro de `parallel {}` retorna `error` y no hay handler `each ->` que lo
maneje → escalación Class B tras completar los despachos. Si `each ->` está definido, el
handler es responsable del manejo de error.

### §6.9 Condicional `if` / `else`

```
if $r.status == "success" {
  [notify] $r.payload.result
}
else {
  [ask-user] retry
}
```

**Gramática:** `if <operando> (==|!=) <operando> {` seguido del bloque *then*, y
opcionalmente `else {` en su propia línea con el bloque *else*. Un operando es exactamente
uno de: una ruta `$ref` (con o sin punto), un literal de string, o `null`.

**Semántica (`evalGuard`):**

- Se resuelven ambos operandos y se aplica la comparación determinista `==`/`!=`.
- `null == null` es **verdadero**. Un `$ref` que no resuelve vale `null`, por lo que
  `$missing == null` es verdadero.
- `null` comparado con un string es **falso** (`!=` lo niega).
- Dos strings comparan por valor.
- Solo `==` y `!=`; solo strings y `null`. No hay operadores compuestos ni aritmética
  (no-objetivo, ver preámbulo).

**Operando compuesto → skip total Class A:** si algún operando resuelve a un valor
compuesto (mapa o lista), la guarda es no comparable: aviso Class A y se **salta el
statement completo** — ninguna rama corre y el programa continúa. No escala.

**Scoping por rama:** la rama seleccionada corre en su propio scope (`Push`/`Pop`). Los
bindings creados dentro de una rama mueren al salir de ella; la otra rama nunca se ejecuta.
Una guarda verdadera con rama `then` vacía es un no-op.

**Comportamiento en resume (§10):** un `if` de nivel superior cuenta como **un** paso
completado. Al reanudar más allá de él, la guarda **no** se re-evalúa y los efectos
laterales de su rama **no** se re-disparan; el resultado final reproduce el de una corrida
limpia.

### §6.10 `parallel foreach` — fan-out dinámico

`parallel foreach` despacha una **plantilla** de wave concurrentemente, una vez por
elemento de una lista de runtime. Es el paralelismo cuyo ancho depende de los datos.

```
parallel foreach $r.items --id=W {
  [echo] : "$item @ $index"
}
  each -> {
    [notify] : "$result"
  }
```

**Gramática:**

- La cabecera es `parallel foreach <$ref> --id=<BASE> {`. El `$ref` es la lista a recorrer
  (admite ruta con punto, §2.8; la ruta se conserva sin el `$`).
- `--id=<BASE>` es **obligatorio** y es el **único** flag admitido en la cabecera; su
  ausencia o cualquier flag extra es error *Syntax*. Una cabecera sin `{`, sin `$ref`, o
  con texto suelto antes del `{` es error *Syntax*.
- El cuerpo contiene **exactamente una** plantilla de despacho. Cero o dos despachos son
  error *Syntax*.
- La plantilla **no** puede llevar su propio `--id`: el runtime sintetiza el id (error
  *Syntax* si lo lleva).
- El handler `each -> { ... }` es opcional.

**IDs sintéticos y determinismo:**

- El runtime sintetiza los ids `<BASE>-1`, `<BASE>-2`, … **1-based** por índice de
  elemento. Dentro de la plantilla, `$item` es el elemento actual y `$index` su índice
  1-based.
- Los despachos corren concurrentemente bajo el semáforo de `max_concurrency`. **El reporte
  y el handler `each` se ensamblan estrictamente en orden de índice**, con independencia del
  orden de completado (determinismo observable). Un `[return]` dentro de `each` emite
  secciones numeradas por índice (`<label>-1`, `<label>-2`, …).
- `$item`/`$index` viven solo durante el statement: referirlos después es un aviso Class A
  (`unbound`).

**Reserva de prefijo (R13):** ningún `--id` **de despacho** literal en el programa —en
cualquier orden textual, incluido dentro de un `parallel {}` estático— puede coincidir con
el patrón `^<BASE>-[0-9]+$` de un fan-out; hacerlo es error *Syntax* de colisión. Los ids
que no calzan el patrón (`W1`, `W-a`, base distinta) no colisionan. Un `--id` de `[return]`
puede coincidir con el patrón **sin** colisionar: solo los ids de despacho reservan.

**Casos límite y escalación:**

- Un `$ref` que **no** resuelve a una lista es un aviso Class A (`not a list`): no se
  despacha nada y el programa continúa.
- Una lista vacía produce cero despachos; el `each` no corre; no hay error.
- Un despacho de ítem que retorna `error` sin handler `each` escala **Class B** (`unhandled
  parallel errors`) tras completar todos los ítems.
- Un comando de plantilla desconocido es Class B (`unknown command`) durante la preparación.
- El seguimiento de dependencias del checkpoint recorre el `$ref` de la lista y toda `$ref`
  de la plantilla y del `each`.

Un `foreach` a solas sigue siendo iteración secuencial (§6.6) y un `parallel {}` estático
(§6.1) queda intacto: solo la secuencia `parallel foreach` dispara el fan-out.

---

## §7 Comportamiento ante errores de parseo y escalación

### §7.1 Categorías de error

| Categoría | Descripción |
|---|---|
| Syntax error | Token mal formado, bloque sin cerrar, escape inválido (`\X`) o literal de string sin cerrar (§1.4), `--id` faltante en `parallel`, cabecera/cuerpo malformado de `parallel foreach` (§6.10), `map()` malformado o clave duplicada (§2.6), `call` sin ruta literal (§2.11), `[return]` con reglas de etiqueta violadas, `[if]`/`while` como forma rechazada |
| Unknown command | `[nombre]` no está en el registry y no es palabra clave |
| Type error | Tipo de argumento incorrecto, binding usado antes de ligarse, `loop limit` no entero o ≤ 0, operación de lista sobre no-lista, `--retry`/`--backoff` negativo |
| Version mismatch | Primera línea no comentario de un `.ann` distinta de `# ann v0.3` (incluye la heredada `# ann v0.2`) |

### §7.2 Parada en el primer error

Ann es *stop-on-first-error* para errores de parseo: al detectar uno, arkannie se detiene
antes de ejecutar cualquier statement. Los despachos `parallel` ya en vuelo se dejan
completar antes de reportar.

### §7.3 Mapeo de clase de escalación

| Situación | Clase |
|---|---|
| Syntax error en `.ann` | B |
| Unknown command | B |
| Type error (parseo o runtime) | A |
| Version mismatch en `.ann` | B |
| `--id` faltante o duplicado en `parallel {}` | B |
| `loop limit` no entero o ≤ 0 | A |
| Guarda de `if`/`until` con operando compuesto | A (skip, no escala) |
| `foreach` sobre binding no-lista | A (skip) |
| `parallel foreach` sobre binding no-lista | A (skip) |
| `[return]` con operando no ligado o ausente | A (skip) |
| `$ref` de elemento irresoluble en `list()`/`concat()`/`map()` | A (omite el elemento) |
| Ruta `$ref` irresoluble en interpolación de `context_block` | B |
| `\X` (escape inválido) o literal de string sin cerrar | B (Syntax) |
| `--retry`/`--backoff` negativo | A |
| `--retry` sobre agente `executor` | B (pre-despacho, no despacha) |
| Ítem de `parallel foreach` con `error` sin handler `each` | B (tras completar todos) |
| `call` a ruta fuera del directorio del programa | B |
| `call` a módulo inexistente o con cabecera de versión incorrecta | B |
| `call` anidado (excede profundidad 1) | B |
| Módulo invocado por `call` que falla | B (en el padre; el resume re-ejecuta el `call`) |

### §7.4 Protocolo completo de clases de error

**Class A — Fallo local, se maneja de forma autónoma.** arkannie resuelve, reporta y
continúa; sin compuerta del desarrollador. Ejemplos: error de tipo, `loop limit ≤ 0`,
guarda con operando compuesto (se salta), `foreach` sobre no-lista, `[return]` no ligado.
Acción: corregir o saltar, emitir un aviso breve, continuar.

**Class B — Riesgo de estado compartido, detenerse y proponer.** arkannie detiene la
ejecución. Si hay un archivo de actividad abierto, escribe `error_state: [descripción]`.
Reporta el fallo completo, propone una vía de recuperación y espera; no ejecuta recuperación
alguna sin instrucción explícita. Ejemplos: wave retorna `error` sin handler, `parallel`
con error no manejado, mismatch de versión, archivo requerido faltante al arranque, binding
irresoluble durante el render del `context_block`.

**Class C — Irreversible, cero recuperación sin instrucción explícita.** arkannie se
detiene de inmediato: sin propuesta, sin escritura de `error_state`, sin ninguna otra
acción. El desarrollador debe dar instrucción explícita con autorización clara. Ejemplos:
toque a sistema productivo, force push a rama protegida, rollback, operación destructiva de
base de datos.

El **formato exacto** de todo mensaje de error de arkannie está normado en
`agent-protocol.md §8` y es no negociable; aquí no se re-especifica.

---

## §8 Estado de las construcciones de Ann v0.3

| Construcción | Estado | Notas |
|---|---|---|
| `[comando] args` | Soportado | Despacho wave o palabra clave nativa |
| `[comando] arg : texto` | Soportado | Bloque de contexto → `context.text` (multilínea §2.7) |
| `$name = [comando]` | Soportado | Binding desde el `payload` de `success` |
| `$name = "literal"` | Soportado | Binding de string literal (comillas y escapes §1.4) |
| `$name = list(...)` | Soportado | Constructor de lista; anida `list()`/`map()` (§2.6) |
| `$name = concat(...)` | Soportado | Concatenación con aplanado de un nivel (§2.6) |
| `$name = map(k: v, ...)` | Soportado | Constructor de mapa ordenado (§2.6) |
| `$name = call "mod.ann"` | Soportado | Composición de módulos como función (§2.11) |
| `\"` / `\\` / `\$` en literales | Soportado | Escapes de string (§1.4) |
| `$ref` / `$ref.seg.seg` | Soportado | Acceso por punto sobre KMap (§2.8) |
| `success -> {}` / `error -> {}` / `info -> {}` | Soportado | Handlers trinarios |
| `parallel {}` + `each ->` | Soportado | Despacho concurrente, plano |
| `parallel foreach $l --id=B {}` + `each ->` | Soportado | Fan-out dinámico determinista (§6.10) |
| `foreach $list {}` | Soportado | Iteración secuencial; admite lista con punto |
| `loop limit=N {}` | Soportado | Bucle acotado |
| `loop limit=N until <guarda> {}` | Soportado | Post-condición determinista (§6.7) |
| `if <guarda> {} else {}` | Soportado | Condicional determinista (§6.9) |
| `--retry=N` / `--backoff=S` | Soportado | Reintento declarativo, solo `agnostic` (§2.10) |
| `[return] <operando>` | Soportado | Indicador de salida (§2.9) |
| `[ask-user]` / `[notify]` / `[clarify]` | Soportado | Palabras clave nativas |
| `[if]` con corchetes / `while` / `[while]` | Rechazado | Usar `if` o `loop` |
| `parallel {}` anidado | No soportado | Solo plano |
| `parallel foreach` con ≠1 plantilla | Rechazado | Exactamente una plantilla (§6.10) |
| `call` anidado / `call` con argumentos | No soportado | v0.4 (no-objetivo); profundidad fija 1 |
| Guardas compuestas (`&&`, `||`, aritmética) | No soportado | v0.4 (no-objetivo) |
| Funciones de usuario (UDF) | No soportado | v0.4 (no-objetivo) |

---

## §9 Esquema canónico del `context_block`

El `context_block` es el payload estructurado que arkannie envía a un wave. arkannie lo
construye antes del despacho, serializado como YAML con orden de clave fijo:

```yaml
operation: <nombre de operación>
context:                # opcional; context.text = texto del bloque de contexto (§2.7)
  text: "..."
flags:                  # opcional; flags booleanos como "nombre", con valor como "nombre=valor"
  - verbose
output_schema: |        # copia verbatim del output_schema de la operación
  success:
    ...
```

Reglas:

- `operation` (string) y `output_schema` (string) son requeridos; `output_schema` ausente es
  un fallo pre-despacho Class B.
- `context: {}` y `flags: []` son válidos.
- Los `$ref` en el texto de contexto se serializan en el despacho según §2.8 y esta misma
  sección: un string se inlinea; un mapa/lista se agrega como campo
  `context.<último-segmento>`.
- Un campo de contexto requerido por la operación que ningún flag ni binding pobló es un
  Class B pre-despacho.

Los detalles del modelo copy-paste del `output_schema` y su regla de drift están normados en
`agent-protocol.md §7`.

---

## §10 Protocolo de checkpoint de RAM

### §10.1 El problema

En modo programa `.ann`, si arkannie se interrumpe entre el despacho de un wave y el uso de
su valor de retorno, el estado de RAM se perdería. Este protocolo previene la pérdida.

### §10.2 Disparo del checkpoint

Se escribe un checkpoint antes de un despacho de **nivel superior** cuando: (1) se ejecuta
en modo programa, y (2) un statement posterior del programa referencia el binding de ese
despacho. El checkpoint captura el snapshot de RAM y el índice del último paso completado.

### §10.3 Esquema del checkpoint

El checkpoint registra el path del programa, el índice del último paso completado
(`last_completed_step`) y un snapshot de los bindings visibles en ese momento. Un `if` de
nivel superior cuenta como un paso completado; los bindings locales de una rama **no**
sobreviven, por lo que nunca entran al snapshot. El esquema de serialización actual no
cambia respecto de la línea heredada.

### §10.4 Recuperación

Al reiniciar tras una interrupción, arkannie busca un checkpoint que coincida con el path
del programa. Si lo encuentra, carga los bindings del snapshot y reanuda en
`last_completed_step + 1`; si no, comienza desde el inicio. Los pasos ya completados (una
asignación, un `if`) **no** se re-ejecutan al reanudar.

### §10.5 Limpieza

El checkpoint se borra al completar el programa con éxito. **No** se borra ante error:
existe precisamente para habilitar la recuperación.

---

## §11 Herramienta `--check` (parse-only)

`arkannie --check <programa.ann>` hace un parseo de **solo sintaxis**, con cero efectos
secundarios: no carga el registry, no corre el healthcheck de claude, no despacha ningún
agente y no escribe `.output/` ni `.mem/`.

- Parseo limpio → imprime una línea `OK` con el descargo explícito **"syntax only — no
  agents were run"** y sale con **exit 0**.
- Error de parseo → lo reporta a stderr en la forma canónica
  `parse error at L:C [categoría]: mensaje` y sale con **exit 1**.
- `--check` es mutuamente excluyente con los flags de ejecución (`--agent`, `--forge`,
  `--detach`, `--interpret`) y requiere un input `.ann`. Cualquier composición inválida es
  un error de uso: **exit 64**, sin ejecutar nada.

El descargo *syntax only* es normativo: un `--check` verde garantiza **solo** que el
programa parsea; no valida existencia de agentes, resolubilidad de bindings ni contratos de
operación.

Los códigos de salida del CLI en general son: `0` éxito · `1` error · `2` info · `64` error
de uso.

---

## Apéndice: trazabilidad spec↔tests

Cada sección normativa nueva o corregida está respaldada por tests; esta tabla es el candado
anti-divergencia. Ante cualquier duda de semántica, el test decide.

| Sección | Comportamiento normado | Tests que lo respaldan |
|---|---|---|
| §2.8 Acceso por punto (gramática y `Resolve`) | Ruta `$name(.seg)*`, `Resolve` sobre KMap, deep-copy | `internal/ram/ram_test.go`: `TestResolve`, `TestRefToken`, `TestResolveDevuelveCopiaProfunda` |
| §2.8 Acceso por punto (parseo, sitios) | La gramática preserva la ruta en args/`[return]`/`foreach`/`list()`/operandos | `internal/ann/parser_test.go`: `TestDottedRefs`; `internal/scheduler/dotaccess_test.go`: `TestAnnParserAcceptsDottedRefs`, `TestDottedRefsEndToEnd`, `TestDotAccessResolveWiring` |
| §2.8 Acceso por punto (interpolación, Class B) | Valor de campo inlineado; ruta irresoluble Class B nombrando base+segmento; descenso en no-mapa sugiere separar el punto | `internal/scheduler/dotaccess_test.go`: `TestDotAccess`; `internal/dispatch/dotaccess_test.go`: `TestContextBlockDotAccess` |
| §2.9 `[return]` (indicador de salida, reglas de etiqueta) | Operando único, YAML para mapa/lista, no ligado → Class A skip, reglas de `--id` en parseo | `internal/ann/parser_test.go`: `TestParseGolden`; `internal/scheduler/dotaccess_test.go`: `TestDotAccessResolveWiring`, `TestDottedRefsEndToEnd` (caso `[return]`) |
| §6.7 `loop ... until` (post-condición) | Guarda tras el cuerpo y antes del Pop; retry-until-success; sin until = limit exacto; compuesto = Class A tratado como no cumplido | `internal/scheduler/until_test.go`: `TestExecLoopUntil`; `internal/ann/parser_test.go`: `TestLoopUntil`, `TestLoopUntilDump` |
| §6.9 `if` / `else` (guarda determinista) | `==`/`!=`, `null==null` verdadero, null==string falso, operando compuesto = skip total Class A, scoping por rama | `internal/scheduler/execif_test.go`: `TestExecIf`, `TestWalkRefsIf`; `internal/ann/parser_test.go`: `TestIfStatements`, `TestIfDump` |
| §6.9 / §10 `if` + resume | El `if` de nivel superior cuenta como un paso; el resume no re-evalúa la guarda ni re-dispara la rama | `internal/scheduler/ifresume_test.go`: `TestIfTopLevelCheckpointResume` |
| §11 `--check` (parse-only) | Exit 0/1/64, descargo *syntax only*, cero efectos secundarios, exclusión con flags de ejecución | `cmd/arkannie/check_test.go`: `TestCheckValidProgram`, `TestCheckParseError`, `TestCheckInvalidCompositions`, `TestParseArgsCheck`, `TestHelpDocumentsCheck` |
| §1.3 palabras clave como texto libre; `until` contextual | `until`/`while` fuera de posición son texto; formas rechazadas | `internal/ann/parser_test.go`: `TestKeywordsAsFreeText`, `TestForeachLoop` |
| §1.4 Comillas y escapes (léxico) | `\"`/`\\` reales, `\$` verbatim, `\X` inválido = Syntax en la col. de la barra, literal sin cerrar | `internal/ann/quoting_test.go`: `TestLexStringEscapes`, `TestEscapedDollarSurvivesParsing` |
| §1.4 Escape `\$` (una pasada, interpolación) | `EscapePlaceholder`/`RestoreEscapes` ocultan `\$` del patrón de ref y lo restauran como `$` literal; la ref real sobrevive | `internal/ram/escape_test.go`: `TestEscapePlaceholderRoundTrip`; `internal/scheduler/datalit_test.go`: `TestEscapedDollarInValues`, `TestEscapedDollarInArgs`; `internal/dispatch/quoting_test.go`: `TestContextBlockEscapedDollar` |
| §2.6 `list()` (anidamiento, elemento con punto) | `list()` anida `list()`/`map()`; elemento con punto es un solo ref | `internal/ann/datalit_test.go`: `TestListNested`, `TestListDottedElement`; `internal/scheduler/datalit_test.go`: `TestListValueNested`, `TestListValueMapElement` |
| §2.6 `concat()` (aplanado de un nivel, orden) | Aplana un nivel, orden estable, no-lista suelto, vacío/único, arg anidado permanece anidado | `internal/ann/datalit_test.go`: `TestConcatBasic`, `TestConcatMixed`, `TestConcatBorders`, `TestConcatNestedListArg`; `internal/scheduler/datalit_test.go`: `TestConcatFlattenOneLevel`, `TestConcatMixedNonList`, `TestConcatBordersValue`, `TestExecAssignConcat` |
| §2.6 `map()` (claves ident, valores, anidamiento) | Entradas ordenadas, clave ident, valor con gramática de elemento, `map()` como elemento, duplicada/malformada = Syntax con L:C | `internal/ann/maplit_test.go`: `TestMapBasic`, `TestMapNested`, `TestMapDuplicateKey`, `TestMapSyntaxErrors`, `TestMapAsElement`, `TestMapAsTextPositional`; `internal/scheduler/maplit_test.go`: `TestMapValueDotPathAndReturn`; `internal/scheduler/datalit_test.go`: `TestAssignMapLit` |
| §2.6 `$ref` de elemento irresoluble = Class A (omite) | Elemento/entrada irresoluble se omite con aviso Class A nombrando el binding (cambio vs v0.2) | `internal/scheduler/datalit_test.go`: `TestUnresolvableInListIsClassA`, `TestUnresolvableInConcatIsClassA`; `internal/scheduler/maplit_test.go`: `TestMapValueUnresolvableOmitted` |
| §1.3 `concat`/`map`/`call` sensibles a posición | Solo constructores antes de `(`/`"`; bare/contexto/string = texto verbatim | `internal/ann/datalit_test.go`: `TestKeywordsAsText`; `internal/ann/maplit_test.go`: `TestMapAsTextPositional`; `internal/ann/call_test.go`: `TestParseCallFreeText` |
| §2.6 seguimiento de deps de checkpoint (walkRefs) | refs en list/concat/map (anidados y top-level) rastreados | `internal/scheduler/datalit_test.go`: `TestWalkRefsTracksListConcat`, `TestWalkRefsTracksTopLevelMap` |
| §2.7 Contexto multilínea (v0.3) | Blanco interno preservado; corta en dedent/`}`/`->`; indentación relativa; blancos finales descartados; primera línea sin indentar = sin contexto | `internal/ann/quoting_test.go`: `TestCollectContextMultiline` |
| §2.10 Reintento declarativo (`--retry`/`--backoff`) | 1+N intentos, solo recuperable/timeout, agotado = capturable, backoff lineal, correctivo no cuenta, executor = Class B pre-despacho, sin retry = 1 intento | `internal/scheduler/retry_test.go`: `TestDeclarativeRetry` |
| §2.11 `call` (parseo) | `*Call` como statement y expresión; requiere ruta literal; `call` fuera de posición = texto | `internal/ann/call_test.go`: `TestParseCallStatement`, `TestParseCallExpression`, `TestParseCallRequiresString`, `TestParseCallFreeText` |
| §2.11 `call` (semántica de función) | Valor único/KMap, bare no liga, RAM aislada, fallo+resume, profundidad 1, traversal, carga, run-dirs, sin fuga al reporte | `internal/scheduler/call_test.go`: `TestCallSingleReturnValue`, `TestCallMultiReturnKMap`, `TestCallBareExecutesNoBinding`, `TestCallRAMIsolation`, `TestCallFailEscalatesResumeReexecutes`, `TestCallDepthGuard`, `TestCallPathTraversal`, `TestCallLoadErrors`, `TestCallRunDirsNamespaced`, `TestCallReturnsDoNotLeakToReport` |
| §2.11 `call` (frontmatter une módulos) | El `agent(s)` del padre pliega los agentes de los módulos invocados (profundidad 1) | `cmd/arkannie/call_frontmatter_test.go`: `TestProgramAgentsIncludesCalledModules` |
| §6.10 `parallel foreach` (parseo, reserva de prefijo) | Header/cuerpo, una plantilla, `--id` base, plantilla sin id, colisión de prefijo `^base-\d+$`, dot-path, regresiones | `internal/ann/fanout_test.go`: `TestParallelForeachParse`, `TestParallelForeachNoDotPath`, `TestParallelForeachEach`, `TestParallelForeachOneTemplate`, `TestParallelForeachIDRequired`, `TestParallelForeachTemplateNoID`, `TestParallelForeachHeaderExtraFlag`, `TestParallelForeachPrefixCollision`, `TestParallelForeachNoFalseCollision`, `TestParallelForeachStaticRegression`, `TestForeachStillFreeStanding`, `TestParallelForeachMalformedHeader`, `TestParallelForeachMalformedBody` |
| §6.10 `parallel foreach` (ejecución, determinismo) | IDs `W-n` 1-based, `$item`/`$index`, reporte en orden de índice, no-lista Class A, lista vacía, semáforo, scope, error sin each = Class B, comando desconocido, walkRefs | `internal/scheduler/fanout_test.go`: `TestFanoutThreeSpawns`, `TestFanoutDeterministicReport`, `TestFanoutNonList`, `TestFanoutEmptyList`, `TestFanoutSemaphoreBound`, `TestFanoutItemScopeGone`, `TestFanoutErrorWithoutEach`, `TestFanoutUnknownCommand`, `TestFanoutWalkRefs` |
| §8 conjunto completo de construcciones v0.3 | Todas las construcciones (incl. v0.3) parsean al AST esperado | `internal/ann/parser_test.go`: `TestParseGolden` (fixture `testdata/ann/all_constructs.ann` ↔ `.golden`) |
