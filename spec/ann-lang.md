# Especificación del lenguaje Ann v0.2

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
capacidades **no** son parte de Ann v0.2 y **no deben** inferirse de este documento; son
material de v0.3 o posterior:

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
- **Determinismo no negociable.** La clasificación de comandos, la resolución de
  bindings, la evaluación de guardas y el ruteo de handlers son deterministas y sin
  estado oculto. Ninguna construcción de v0.2 introduce no-determinismo.
- **Fuera de alcance en v0.2** (reservado a v0.3): fan-out dinámico (paralelismo cuyo
  ancho depende de datos en runtime), composición/anidamiento de bloques de control,
  reintento declarativo más allá del patrón `loop ... until`, construcción de datos
  estructurados dentro de Ann (más allá de `list()`), y un sistema formal de comillas o
  escapes. Las plantillas `{{ ... }}` dentro del texto de usuario tampoco se resuelven
  (ver §5).

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
# ann v0.2
```

El `#` **debe** ser el primer carácter de la línea (columna 0). Un valor distinto de
`# ann v0.2` es un error de parseo de categoría *Version mismatch* (Class B). En modo
prompt (interactivo contra un solo agente) la cabecera es opcional y se ignora si está
presente.

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
```

- Los bindings son locales a RAM (ver §4).
- El lado izquierdo es `$identificador` — alfanumérico más `_`, sin `-`; no puede ser una
  palabra reservada de §1.3.
- El lado derecho es un átomo de comando, un literal de string o un constructor `list()`.
- Una asignación no produce salida; el resultado se guarda solo en RAM.
- Si el comando retorna `error`, el binding **no** se liga y aplican las reglas de
  escalación de error. Si el comando retorna `success`, el binding recibe el `payload`.

### §2.4 Panorama de control de flujo

Las construcciones de control se detallan en §6. Formas admitidas:

- `parallel { ... } [each -> { ... }]` — despacho concurrente (§6.1–§6.5, §6.8).
- `foreach $lista { ... }` — iteración secuencial (§6.6).
- `loop limit=N [until <guarda>] { ... }` — repetición acotada con post-condición
  opcional (§6.7).
- `if <guarda> { ... } [else { ... }]` — condicional determinista (§6.9).

### §2.5 String interpolado

```
"texto con referencias $binding y $binding.campo"
```

- `$binding` — resuelto desde RAM en tiempo de ejecución (§2.8).
- Los slots `{{ clave }}` que aparezcan dentro del texto de usuario **no** se resuelven en
  v0.2: se transportan verbatim (ver §5). La única sustitución dentro de texto de usuario
  es `$ref`/acceso por punto.

### §2.6 Constructor `list()`

```
$items = list("alpha", "beta", "gamma")
$items = list($a, $b, $c)
```

- Crea un binding de tipo lista.
- Los elementos son literales de string o referencias `$ref` (incluidas rutas con punto,
  ver §2.8).
- Un `$ref` de elemento que no resuelve se sustituye por un elemento string vacío (no
  escala).
- Las listas son inmutables tras su creación.

### §2.7 Bloque de contexto

Un átomo de comando puede ir seguido de un bloque de contexto: texto libre que el agente
wave interpreta para extraer la información que necesita.

```
[comando] arg1 --flag1 : el texto de contexto va aquí
```

El `: ` (dos puntos + espacio) separa la cabecera estructurada del bloque de contexto.

- **Una línea:** el contexto termina al fin de línea.
- **Multilínea:** el contexto continúa en las líneas indentadas siguientes hasta una línea
  en blanco o un token de handler `->`.
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
re-despacho automático en v0.2.

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

---

## §3 Clasificador de instrucciones

La clasificación es determinista. Para cada statement, arkannie decide en este orden y
toma la primera coincidencia:

```
1. Palabra clave de control de flujo (parallel, foreach, loop, if)  → se maneja localmente
2. Palabra clave nativa (ask-user, notify, clarify, return)          → la ejecuta el runtime
3. [comando] resuelto contra el registry de agentes (.agents/)       → despacho wave (proceso claude)
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
despacho o literales de string) **no** se resuelven en v0.2: se transportan verbatim. La
única sustitución dentro de texto de usuario es `$ref`/acceso por punto (§2.8). Un motor de
slots de usuario con condicionales (`{{#if}}`) y fallbacks (`{{ clave | ... }}`) **no** es
parte de v0.2 (no-objetivo).

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

---

## §7 Comportamiento ante errores de parseo y escalación

### §7.1 Categorías de error

| Categoría | Descripción |
|---|---|
| Syntax error | Token mal formado, bloque sin cerrar, `--id` faltante en `parallel`, `[return]` con reglas de etiqueta violadas, `[if]`/`while` como forma rechazada |
| Unknown command | `[nombre]` no está en el registry y no es palabra clave |
| Type error | Tipo de argumento incorrecto, binding usado antes de ligarse, `loop limit` no entero o ≤ 0, operación de lista sobre no-lista |
| Version mismatch | Primera línea no comentario de un `.ann` distinta de `# ann v0.2` |

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
| `[return]` con operando no ligado o ausente | A (skip) |
| Ruta `$ref` irresoluble en interpolación de `context_block` | B |

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

## §8 Estado de las construcciones de Ann v0.2

| Construcción | Estado | Notas |
|---|---|---|
| `[comando] args` | Soportado | Despacho wave o palabra clave nativa |
| `[comando] arg : texto` | Soportado | Bloque de contexto → `context.text` |
| `$name = [comando]` | Soportado | Binding desde el `payload` de `success` |
| `$name = "literal"` | Soportado | Binding de string literal |
| `$name = list(...)` | Soportado | Constructor de lista |
| `$ref` / `$ref.seg.seg` | Soportado | Acceso por punto sobre KMap (§2.8) |
| `success -> {}` / `error -> {}` / `info -> {}` | Soportado | Handlers trinarios |
| `parallel {}` + `each ->` | Soportado | Despacho concurrente, plano |
| `foreach $list {}` | Soportado | Iteración secuencial; admite lista con punto |
| `loop limit=N {}` | Soportado | Bucle acotado |
| `loop limit=N until <guarda> {}` | Soportado | Post-condición determinista (§6.7) |
| `if <guarda> {} else {}` | Soportado | Condicional determinista (§6.9) |
| `[return] <operando>` | Soportado | Indicador de salida (§2.9) |
| `[ask-user]` / `[notify]` / `[clarify]` | Soportado | Palabras clave nativas |
| `[if]` con corchetes / `while` / `[while]` | Rechazado | Usar `if` o `loop` |
| `parallel {}` anidado | No soportado | Solo plano en v0.2 |
| Guardas compuestas (`&&`, `||`, aritmética) | No soportado | v0.3 (no-objetivo) |
| Fan-out dinámico, composición, retry declarativo, construcción de datos, comillas formales | No soportado | v0.3 (no-objetivo) |

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
| §8 conjunto completo de construcciones v0.2 | Todas las construcciones parsean al AST esperado | `internal/ann/parser_test.go`: `TestParseGolden` (fixture `testdata/ann/all_constructs.ann` ↔ `.golden`) |
