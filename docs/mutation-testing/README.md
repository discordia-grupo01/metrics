# Mutation testing de metrics

## Qué es

La cobertura dice qué líneas **se ejecutan** durante los tests, pero no si los
tests **revisan** lo que pasa en ellas. El mutation testing mete a propósito
errores chicos en el código, uno por vez (cambia `>` por `>=`, niega un `if`,
cambia `||` por `&&`...), y corre los tests con cada uno. Si algún test falla,
el error quedó **detectado** (`KILLED`); si todos pasan, **sobrevivió**
(`LIVED`). La **efectividad** es detectados / (detectados + sobrevivientes).

Usamos [gremlins](https://github.com/go-gremlins/gremlins), para Go, sobre
`internal/service` (estadísticas y estado de la plataforma) e
`internal/health` (el monitor que consulta los health checks y decide cuándo un
servicio está `degraded`), con todos los mutantes opcionales activados.

## Cómo correrlo

```bash
go install github.com/go-gremlins/gremlins/cmd/gremlins@latest
make mutation
```

Tarda menos de un minuto y no está en el CI.

**Ojo con los `TIMED OUT`.** La primera corrida de `internal/health` dio 13.
Los tests del monitor esperan a que cambie de estado, y un mutante que lo
rompe los hace esperar hasta su propio límite, más de lo que gremlins espera
por defecto. Con `--timeout-coefficient 30` (y la caché de tests limpia, para
que gremlins mida bien cuánto tardan) los 13 resultaron detectados. Muchos
timeouts no son una buena noticia: pueden esconder sobrevivientes.

## Resultados

Archivos `service-*` e `health-*`, `before` y `after`.

| | `service` antes | `service` después | `health` antes | `health` después |
|---|---|---|---|---|
| Detectados | 30 | 31 | 32 | 44 |
| Sobrevivientes | 1 | 0 | 8 | 0 |
| No cubiertos | 2 | 2 | 4 | 0 |
| **Efectividad** | **96,77%** | **100%** | **80%** | **100%** |

### Lo que faltaba (ya está agregado)

| Dónde | El error que nadie detectaba | Test nuevo |
|---|---|---|
| `service.go:110` | Rechazar un rango de exactamente 366 días (el máximo) | `TestParsePeriodAllowsExactlyTheMaximumRange` |
| `monitor.go:56-65` | Que una configuración en 0 o negativa no use los valores por defecto (intervalo de 30 s, 2 fallas y 2 éxitos seguidos, timeout de 3 s), o que un 1 no se respete | `TestNewMonitorDefaults` |
| `monitor.go:70-71`, `190` | Monitorear un servicio sin nombre o sin URL, o dejar de leer la lista después de uno incompleto | `TestNewMonitorSkipsIncompleteTargets`, `TestParseTargetsSkipsEntriesWithoutNameOrURL` |
| `monitor.go:176` | Tomar un 200 como falla, o un 300 como éxito: solo los 2xx son "sano" | `TestStatusErrorAcceptsOnly2xx` |

El test de los targets incompletos los pone **antes** de los válidos: si
estuvieran al final, cortar la lectura ahí no cambiaría nada y el error no se
notaría.

Los 2 no cubiertos de `service` (`service.go:61` y `66`) son chequeos de error que ningún test ejercita: gremlins no prueba código que no se ejecuta.
