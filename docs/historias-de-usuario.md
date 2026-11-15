# Historias de usuario

Las historias del catálogo de la cátedra que resuelve este servicio, con su número. Las rutas completas con sus parámetros y respuestas están en [`openapi.yaml`](../openapi.yaml). Todas son solo para el staff de la plataforma.

## Optativas

| # | Historia | Puntos | Rutas |
|---|---|---|---|
| 70 | Ver métricas de actividad de un servidor | 5 | `GET /v1/servers/{serverId}/metrics` |
| 71 | Ver métricas globales de la plataforma | 5 | `GET /v1/metrics/global` |
| 72 | Gráfico de crecimiento de miembros | 3 | `GET /v1/servers/{serverId}/metrics/growth` |
| 73 | Exportar métricas a CSV | 3 | `GET /v1/servers/{serverId}/metrics/export.csv` |
| 74 | Estado de la plataforma e incidentes | 5 | `GET /v1/status`, `GET /v1/health/services`, `POST /v1/metrics/incidents`, `POST /v1/metrics/incidents/{id}/resolve` |

## Historias en las que participa

| # | Historia | Qué hace este servicio |
|---|---|---|
| 74 | Estado de la plataforma e incidentes | Publica cada cambio de estado (`platform.status_changed` y `platform.health_changed`) para que `notifications` se lo avise a los clientes en vivo. |
