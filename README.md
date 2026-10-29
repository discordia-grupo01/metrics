# discordia-metrics

Servicio de métricas y estado de plataforma. Consume eventos de
`discordia.events` y mantiene una proyección propia en PostgreSQL; no accede a
las bases de identity, servers o messaging.

## Funcionalidad

- `GET /v1/servers/{serverId}/metrics`: miembros activos, mensajes enviados y
  nuevos miembros en un período.
- `GET /v1/servers/{serverId}/metrics/growth`: evolución diaria de miembros.
- `GET /v1/servers/{serverId}/metrics/export.csv`: exportación CSV del resumen.
- `GET /v1/metrics/global`: usuarios activos, servidores creados y mensajes
  enviados para staff.
- `GET /v1/status`: incidentes activos para usuarios autenticados.
- `POST /v1/metrics/incidents` y
  `POST /v1/metrics/incidents/{id}/resolve`: publicar y resolver incidentes
  para staff.

Los endpoints de servidor consultan `GET /v1/servers/{serverId}/members/{userId}/permissions`
en servers para verificar `ADMINISTRATOR`; no aceptan un usuario enviado por el
cliente. Las métricas globales e incidentes usan `STAFF_USER_IDS`, que debe
contener los UUID de staff provistos por el despliegue seguro. En desarrollo
local se puede configurar `USER_ID_HEADER=X-User-Id`; en producción la identidad
proviene del JWT validado por Kong.

Si no hay eventos en un período, el resumen devuelve `has_activity: false` y
los endpoints de crecimiento devuelven `points: []`, permitiendo que el cliente
muestre el contexto de “sin actividad”.

## Desarrollo local

```bash
cp .env.example .env
docker compose up --build
```

La API queda en `http://localhost:8080`; la base en `localhost:5432` y
RabbitMQ en `localhost:5672`. Las migraciones se ejecutan mediante el servicio
`migrations` antes de iniciar la API.

## Estructura

```text
cmd/api/                 configuración, router y ciclo de vida
internal/handler/        adaptadores HTTP
internal/service/        períodos, autorización y exportación
internal/repository/     PostgreSQL y agregaciones
internal/events/         consumidor RabbitMQ idempotente
internal/middleware/     identidad derivada del JWT/header local
migrations/              esquema de proyección e incidentes
```

## Eventos consumidos

`servers.member_joined`, `servers.member_left`, `servers.server_updated` y
`messaging.message_created`. El `event_id` es clave primaria, por lo que las
entregas duplicadas no inflan las métricas. Los eventos inválidos se reencolan
para no perderlos silenciosamente.

## Validación

```bash
gofmt -l .
go vet ./...
go test ./... -cover
go build ./...
```

El workflow de GitHub Actions ejecuta estas verificaciones en cada push y pull
request. El compose de [infrastructure](../infrastructure/) agrega PostgreSQL,
migraciones, RabbitMQ y el servicio al stack completo; Kong expone las rutas
con JWT.
