# arkannie — Manual de pruebas pendientes (live / manual)

La suite automatizada (`make test`) es verde y cubre parser, RAM, checkpoints,
registry, dispatch, envelope, scheduler, output, CLI, spawn y template con
stubs — **sin** tocar el `claude` real. Este documento cataloga las pruebas que
**no** están en la suite: smoke *live* contra claude real, flujos interactivos
(TTY) y checklists semi-manuales. Se documentan aquí a medida que se ejecutan.

**Convención de estado:** ⬜ pendiente · ✅ pasó · ❌ falló · 🔁 hay que re-correr
(el binario cambió desde la última vez).

> **Recordatorio:** el shim ejecuta el binario compilado, no recompila. Antes de
> cualquier smoke: `make build`. Anota siempre la fecha y el SHA del binario.

---

## A. Smoke *live* contra claude real

Requieren el CLI de `claude` en el PATH y una cuenta activa. Producen un
`.output/<id>.md` real.

| ID | Qué prueba | Cómo correr | Esperado | Estado | Última corrida |
|---|---|---|---|---|---|
| L1 | Dispatch básico (echo) end-to-end | `arkannie --agent=echo --id=smoke "hola mundo"` | exit 0; `.output/smoke.md` con `status: success`, payload `{echo: hola mundo}` | 🔁 | 2026-07-09 (verde, pero binario cambió: falta re-correr) |
| L2 | Payload tipado vs `output_schema` | `arkannie --agent=echo --id=smoke2 "texto"` | payload `{echo:<texto>}` casa con `success:{echo:string}`; exit 0 | 🔁 | 2026-07-09 (verde en payload-contract) |
| L3 | Directivas de flag (groups/personality/modifiers) | `arkannie --agent=echo --id=dir --backwards --terse --personality=techlead "hola"` | la salida refleja el orden de directivas (reverse + terse + framing techlead) | ⬜ | — (opcional, flag-directives) |
| L4 | Ciclo `--interpret` (reparación de programa) | correr un `.ann` con error de parse + `--interpret` | claude repara una vez; el programa corregido aparece verbatim en el output | ⬜ | — |
| L5 | `parallel {}` con mismatch → `each ->` | `.ann` con dispatch que viola schema dentro de `parallel` | el mismatch se enruta al handler `each`; sin regresión | ⬜ | — (automatizado con stub; falta live) |

## B. Flujos interactivos (requieren TTY + claude)

El binario solo pre-valida y ensambla el prompt semilla; la sesión de diseño la
conduce claude interactivo. No automatizables en CI.

| ID | Qué prueba | Cómo correr | Esperado | Estado | Última corrida |
|---|---|---|---|---|---|
| I1 | Agent Forge crea un agente válido | `arkannie --forge` (o `--forge=<n>`) | produce `.agents/<n>/` que pasa `validate` sin edición manual; incluye bloque `capabilities` | ⬜ | — |
| I2 | Absorción — modo complete | `arkannie --forge=<n> --absorb=<ruta> --mode=complete` | 7 fases con gates; 1 agente con N operations; smoke dispatch verde | ⬜ | — (gap G4) |
| I3 | Absorción — modo fragment | `arkannie --forge=<n> --absorb=<ruta> --mode=fragment` | N agentes + `.ann` de recomposición; corre con `--id=absorb-smoke` | ⬜ | — (gap G4) |
| I4 | Absorción — modo layer | `arkannie --forge=<n> --absorb=<ruta> --mode=layer` | agente con `layer.origin`; dispatch requiere `--allow-layer`; corre con `cwd=origin` | ⬜ | — (gap G4) |
| I5 | Consentimiento layer | despachar un agente layer sin/con `--allow-layer` | sin consent → error Class B atrapable; con consent → corre en el origen | ⬜ | — (automatizado con stub; falta live) |

## C. Empaquetado e instalación

| ID | Qué prueba | Cómo correr | Esperado | Estado | Última corrida |
|---|---|---|---|---|---|
| P1 | Build limpio | `make build` | `bin/arkannie` producido, sin errores | ✅ | 2026-07-11 |
| P2 | Install del shim | `make install` && `arkannie --catalog` | symlink en `~/.local/bin/arkannie`; ejecuta el binario | ✅ | 2026-07-11 |
| P3 | Empaquetado dist | `make dist` && untar en otra ruta | `dist/arkannie-<v>.tar.gz` autocontenido; `arkannie --catalog` corre desde el untar | ✅ | 2026-07-11 |
| P4 | Suite automatizada | `make test` | gofmt + vet + `go test -race -cover` verde (cobertura ≥ 80%) | ✅ | 2026-07-11 |

## D. Consultas deterministas (sin claude)

| ID | Qué prueba | Cómo correr | Esperado | Estado | Última corrida |
|---|---|---|---|---|---|
| D1 | Catálogo del pool | `arkannie --catalog` | renderiza la carta de cada agente válido; exit 0 | ✅ | 2026-07-11 |
| D2 | Catálogo de un agente | `arkannie --catalog=echo` / `--catalog=nope` | uno → exit 0; inexistente → exit 64 | ✅ | 2026-07-11 |
| D3 | Validación de contratos | `arkannie validate` | `OK: N agent(s) valid`; exit 1 si hay violaciones | ✅ | 2026-07-11 |
| D4 | Rutas usage-error de forge/absorb | `--absorb` sin `--forge`, `--mode` inválido, ruta inexistente, etc. | exit 64 en cada caso, sin cargar el registry | ✅ | 2026-07-11 |

## E. Checklists semi-manuales (del plan original)

| ID | Qué prueba | Estado | Nota |
|---|---|---|---|
| U1 | Spike de contención (agnostic confinado por omisión de tools + belt) | ⬜ | semi-manual; verificar que un agente agnostic no puede escribir/ejecutar |
| U16-T4 | Checklist de aceptación end-to-end | ⬜ | recorrer el flujo completo con un caso real |

---

## Cómo documentar una corrida

Al ejecutar una prueba, actualiza su fila: estado (✅/❌), fecha, y si falló,
enlaza el `.output/<id>.md` o pega el error. Para smoke live, anota además el
SHA del binario (`git rev-parse --short HEAD` tras `make build`).
