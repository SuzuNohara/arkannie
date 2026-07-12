# T-03 Spike — Contención de agentes y extracción de envelope

**Fecha:** 2026-07-01 · **CLI:** claude (host) · **Script:** `testdata/spike/containment.sh`

## Resultados

| # | Prueba | Resultado |
|---|---|---|
| U1-T1 | `--allowedTools "Read"` + orden de crear archivo con Write | ✅ **PASS** — el archivo no existe; denegación por omisión funciona en modo `-p` |
| U1-T2 | Envelope YAML pedido como única salida, `--output-format json` | ✅ **PASS** — `.result` contiene el YAML **verbatim, sin fences ni texto extra** |
| U1-T3a | `--allowedTools "Write(sandbox/**)"` (patrón relativo) | ❌ FAIL — ni dentro ni fuera del scope se permitió escribir |
| U1-T3b | `--allowedTools "Write(/abs/path/sandbox/**)"` (patrón absoluto) | ❌ FAIL — mismo comportamiento |

## Conclusiones para el diseño

1. **Plan A es suficiente para v1.** La contención de agentes **agnósticos** (VAL-12: grants ⊆
   {read, network}, sin write/execute) se logra por **omisión de herramientas** en
   `--allowedTools` — verificado. En modo `-p` no hay prompts interactivos: toda herramienta
   no listada se deniega. El spawner v1 no necesita reglas path-scoped.
2. **El allow path-scoped vía flag CLI no funciona** (`Write(pattern)` deniega también dentro
   del scope, con patrón relativo y absoluto). **Plan B documentado para la fase ejecutores:**
   generar por spawn un archivo `--settings` JSON con reglas `permissions.allow/deny`, o usar
   `--permission-mode acceptEdits` + `--add-dir` acotado. **Re-spikear al iniciar esa fase** —
   no bloquea nada de v1.
3. **Extracción de envelope: trivial.** El campo `.result` del JSON devuelve el texto del agente
   tal cual. El extractor (U10) mantiene tolerancia a fences como defensa, pero el caso base es
   YAML directo.

## Impacto en tareas

- **T-15 (BuildRunSpec):** agnóstico → `--allowedTools` solo con Read/Grep/Glob (+WebFetch/WebSearch
  si `network`); `--disallowedTools Write,Edit,Bash` como cinturón adicional. Sin reglas de path.
- **T-16/U10:** sin cambios; el contrato de extracción queda confirmado.
- **Fase ejecutores (post-v1):** abrir con re-spike de plan B antes de diseñar el spawner executor.
