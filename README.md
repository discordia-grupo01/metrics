# discordia-metrics

Servicio de métricas de actividad y estado de plataforma de Discordia. Mantiene
una proyección local de eventos de RabbitMQ y expone una API HTTP para
administradores de servidores y staff de la plataforma.

## Historias cubiertas

- Ver métricas de actividad de un servidor.
- Informar explícitamente cuando un período no tuvo actividad.
- Restringir las métricas de servidor a usuarios con `ADMINISTRATOR`.
- Ver métricas globales para usuarios de staff.
- Consultar la evolución diaria de miembros.
- Exportar el resumen de métricas de un servidor a CSV.
- Publicar, consultar y resolver incidentes de la plataforma.

## Stack

- Lenguaje: Go 1.22.
- Framework HTTP: Gin.
- Base de datos: PostgreSQL 16.
- Eventos: RabbitMQ, exchange `discordia.events`.
- Autenticación externa: Kong valida el JWT; el servicio obtiene el usuario del
  claim `sub`.
- Autorización de servidor: proyección local alimentada por eventos de
  `servers`; no realiza consultas HTTP request-by-request.
- Imagen: Docker multi-stage basada en `golang:1.22-alpine`.

El servicio es dueño de su proyección y de los incidentes. No accede
directamente a las bases de datos de `identify`, `servers` o `messaging`.

## Configuración

Las variables reales se configuran mediante `.env` o el entorno del despliegue.
No se deben commitear archivos `.env`, contraseñas, tokens ni credenciales.
Usar [.env.example](.env.example) como plantilla.

| Variable | Default | Descripción |
|---|---|---|
| `ENVIRONMENT` | `development` | Entorno de ejecución. En `production` Gin usa release mode. |
| `HOST` | `0.0.0.0` | Dirección de escucha HTTP. |
| `PORT` | `8080` | Puerto HTTP. |
| `DATABASE_HOST` | `localhost` | Host de PostgreSQL. |
| `DATABASE_PORT` | `5432` | Puerto de PostgreSQL. |
| `DATABASE_NAME` | `discordia_metrics` | Base de datos de la proyección. |
| `DATABASE_USER` | `discordia` | Usuario de PostgreSQL. |
| `DATABASE_PASSWORD` | `discordia` | Contraseña de PostgreSQL. En producción debe ser obligatoria y secreta. |
| `DATABASE_SSLMODE` | `disable` | `disable` en local; usar `require` o el modo indicado por el proveedor en producción. |
| `DATABASE_URL` | vacío | Cadena PostgreSQL completa. Si está definida, tiene prioridad sobre las variables `DATABASE_*`. |
| `RABBITMQ_URL` | vacío | URL AMQP. Si está vacía, la API arranca pero no consume eventos. |
| `EVENTS_EXCHANGE` | `discordia.events` | Exchange topic compartido por los servicios. |
| `EVENTS_QUEUE` | `metrics` | Cola durable exclusiva de esta proyección. |
| `STAFF_USER_IDS` | vacío | Bootstrap/fallback temporal de UUIDs de staff. La fuente principal son los eventos firmados de `admin`. |
| `ADMIN_EVENTS_HMAC_SECRET` | vacío | Secreto compartido con `admin` para validar cambios de staff recibidos por RabbitMQ. |
| `USER_ID_HEADER` | vacío | Header de identidad para desarrollo local. No configurarlo en producción. |
| `HEALTH_CHECKS` | vacío | Lista separada por comas de `servicio=URL/readyz` que metrics consulta para mostrar el estado operativo. |
| `HEALTH_CHECK_INTERVAL` | `30s` | Intervalo entre consultas de health checks. |
| `HEALTH_CHECK_TIMEOUT` | `3s` | Timeout de cada consulta. |
| `HEALTH_FAILURE_THRESHOLD` | `2` | Fallos consecutivos necesarios para marcar un servicio como `degraded`. |
| `HEALTH_RECOVERY_THRESHOLD` | `2` | Respuestas exitosas consecutivas necesarias para recuperar un servicio degradado. |

### Identidad y autorización

En el flujo normal, Kong valida la firma y expiración del JWT y enruta la
solicitud al servicio. Metrics lee el claim `sub` del bearer token. No confía
en un `user_id` enviado como parámetro o en el cuerpo.

Para métricas de un servidor, metrics verifica la proyección local de
servidores, propietario, roles y roles asignados. Se permite únicamente al
propietario o a un usuario con un rol que tenga `ADMINISTRATOR`. Si la
proyección aún no conoce el servidor o está atrasada, se deniega el acceso de
forma segura; no se consulta a `servers` como fallback.

Las métricas globales y la gestión de incidentes consultan una proyección local
de los miembros de staff publicada por `admin` mediante eventos RabbitMQ
firmados. `STAFF_USER_IDS` queda como bootstrap/fallback temporal para no
bloquear despliegues durante la migración.

`USER_ID_HEADER` existe únicamente para desarrollo local y pruebas manuales. No
debe configurarse en un entorno expuesto: cualquier cliente que pueda enviar
ese header podría suplantar la identidad.

## API

El contrato completo está en [openapi.yaml](openapi.yaml). Todas las rutas
`/v1/**` requieren autenticación, y Kong aplica JWT antes de llegar al
servicio.

### Métricas de un servidor

- `GET /v1/servers/{serverId}/metrics`
- `GET /v1/servers/{serverId}/metrics/growth`
- `GET /v1/servers/{serverId}/metrics/export.csv`

Los parámetros `from` y `to` son timestamps RFC3339. Si se omiten, se usa el
período de los últimos 30 días. El período máximo permitido es de 366 días y
`from` debe ser anterior a `to`.

El resumen de servidor incluye:

- `active_members`: usuarios distintos que generaron actividad registrada en el
  período. Se consideran actividad los mensajes y los eventos de alta o baja
  de membresía;
- `messages_sent`: mensajes registrados por `messaging.message_created`;
- `new_members`: altas registradas por `servers.member_joined`;
- `has_activity`: indica si alguna de esas métricas es distinta de cero.

Cuando no existe actividad, la respuesta conserva `200 OK`, devuelve contadores
en cero y `has_activity: false`. El cliente puede mostrar un mensaje
contextualizado en lugar de un gráfico vacío.

El endpoint de crecimiento devuelve puntos diarios. Los días sin cambios se
incluyen con el total calculado para ese día.

La exportación devuelve `text/csv` con las columnas:

```text
from,to,active_members,messages_sent,new_members
```

### Métricas globales

`GET /v1/metrics/global` requiere staff y devuelve un resumen agregado de todos
los servidores dentro del período solicitado. Su respuesta es distinta del
resumen de servidor e incluye:

- `active_users`: usuarios distintos que generaron mensajes o cambios de
  membresía;
- `servers_created`: servidores cuyo primer evento `server_updated` ocurrió en
  el período;
- `messages_sent`: mensajes registrados por `messaging.message_created`;
- `has_activity`: indica si alguna de esas métricas es distinta de cero;
- `granularity` y `series`: los mismos tres contadores divididos en tramos
  para graficar. Si el período dura hasta 24 horas los tramos son por hora
  (`hour`); si es más largo, por día (`day`). Cada punto trae `start`, el
  inicio del tramo en UTC. Los tramos arrancan en `from`, así que un cliente
  que manda su medianoche local recibe días alineados con su zona horaria.
  Los usuarios activos de cada tramo no suman el total del período.

### Estado e incidentes

- `GET /v1/status`: devuelve el estado actual (`operational`, `degraded` o
  `maintenance`) y los incidentes activos para cualquier usuario
  autenticado.
- `GET /v1/health/services`: devuelve a staff el último estado observado de
  cada servicio configurado. Es una señal para sugerir un incidente; no crea
  incidentes automáticamente.
- `POST /v1/metrics/incidents`: crea un incidente; requiere staff.
- `POST /v1/metrics/incidents/{id}/resolve`: resuelve un incidente activo;
  requiere staff.

Al publicar o resolver un incidente, metrics publica `platform.status_changed`
en RabbitMQ cuando `RABBITMQ_URL` está configurada. El payload solo contiene el
estado actual y un `event_id`; notifications lo convierte en un broadcast
`status_changed` sobre el tópico `platform` con `refetch: true`. El evento es
un aviso y no reemplaza `GET /v1/status`, que sigue siendo la fuente de verdad.

Un incidente contiene título, descripción, servicios afectados y el estado de
plataforma que representa (`degraded` por defecto o `maintenance`). Al
resolverlo deja de aparecer en `GET /v1/status`; si no quedan incidentes
activos, la plataforma vuelve a `operational`.

## Eventos consumidos

Metrics declara la cola `EVENTS_QUEUE` y la vincula al exchange
`EVENTS_EXCHANGE` con estas routing keys:

| Routing key | Proyección |
|---|---|
| `servers.member_joined` | Registra un alta de miembro. |
| `servers.member_left` | Registra una baja de miembro. |
| `servers.server_updated` | Registra la creación/actualización publicada por servers. |
| `servers.server_deleted` | Marca el servidor como eliminado en la proyección de autorización. |
| `servers.role_updated` | Actualiza si un rol contiene `ADMINISTRATOR`. |
| `servers.role_deleted` | Marca un rol como eliminado. |
| `servers.member_roles_updated` | Reemplaza los roles asignados a un usuario. |
| `servers.owner_updated` | Actualiza el propietario del servidor. |
| `messaging.message_created` | Registra un mensaje sin almacenar su texto. |

Cada evento debe tener un `event_id` UUID y `occurred_at`. `event_id` es clave
primaria en `metric_events`, por lo que las entregas duplicadas son
idempotentes. Los eventos inválidos se descartan con `Nack(requeue=false)` para
evitar reintentos infinitos; los errores transitorios de persistencia se
reencolan.

La entrega es eventualmente consistente: una métrica puede tardar en reflejar
un evento hasta que RabbitMQ lo entregue y el consumidor lo persista.

### Códigos de error

- `400`: período o cuerpo inválido;
- `401`: falta autenticación;
- `403`: el usuario autenticado no tiene el permiso requerido, o la
  proyección local todavía no conoce el servidor;
- `404`: el incidente no existe o ya fue resuelto;
- `500`: error interno no clasificado.

## Persistencia y migraciones

Las migraciones ordenadas están en [migrations](migrations). La migración
inicial crea:

- `metric_events`, la proyección de actividad;
- índices por servidor, tipo y fecha;
- `incidents`, el estado de incidentes de plataforma;
- `metric_servers`, `metric_roles`, `metric_member_roles` y `metric_owners`,
  la proyección de autorización local.

En desarrollo standalone, Docker Compose ejecuta el servicio `migrations`
antes de iniciar `api`. Para ejecutar una migración manual:

```bash
set DATABASE_URL=postgres://discordia:discordia@localhost:5432/discordia_metrics?sslmode=disable
make migrate
```

En PowerShell se puede definir la variable con:

```powershell
$env:DATABASE_URL = "postgres://discordia:discordia@localhost:5432/discordia_metrics?sslmode=disable"
make migrate
```

## Despliegue en Render

El servicio puede configurarse en Render como un **Web Service** usando el
`Dockerfile` de este directorio. Activar el deploy automático para la rama
`main`; el workflow de GitHub Actions valida en cada pull request que la
imagen compile y que el contenedor arranque junto con sus dependencias.

Configurar estas variables en Render:

| Variable | Valor recomendado |
|---|---|
| `ENVIRONMENT` | `production` |
| `HOST` | `0.0.0.0` |
| `PORT` | La variable `PORT` proporcionada por Render; si Render no la inyecta, usar `8080`. |
| `DATABASE_HOST` | Host interno de PostgreSQL |
| `DATABASE_PORT` | Puerto interno de PostgreSQL, normalmente `5432` |
| `DATABASE_NAME` | `discordia_metrics` |
| `DATABASE_USER` | Usuario de PostgreSQL |
| `DATABASE_PASSWORD` | Secret de PostgreSQL |
| `DATABASE_SSLMODE` | `require` (o el modo indicado por el proveedor) |
| `RABBITMQ_URL` | URL AMQP del RabbitMQ desplegado; dejar vacía solo si no se consumirán eventos |
| `EVENTS_EXCHANGE` | `discordia.events` |
| `EVENTS_QUEUE` | `metrics` |
| `STAFF_USER_IDS` | UUIDs de staff separados por comas |

No es necesario configurar `JWT_SECRET`: la validación del JWT pertenece a
Kong y metrics recibe la identidad ya validada. Tampoco configurar
`USER_ID_HEADER` en producción, porque permite suplantar la identidad.

Configurar `/readyz` como health check de Render. Antes del primer deploy y
cada vez que cambie el esquema, ejecutar las migraciones contra la base de
producción desde un entorno que tenga instalado `migrate`:

```text
migrate -path migrations -database "postgres://<usuario>:<password>@<host>:<puerto>/<base>?sslmode=require" up
```

La imagen actual contiene únicamente el binario de la API; por eso el comando
anterior no está disponible dentro de ella. Render no obtiene automáticamente
las migraciones desde `docker-compose.yml`, que es para desarrollo local y el
smoke test del CI. Usar un job de migración separado o agregar explícitamente
el CLI a una imagen de pre-deploy, manteniendo las variables de conexión como
secrets.

### Usar Supabase como PostgreSQL

Para conectar `metrics` a la base PostgreSQL de Supabase, copiar desde
Supabase Dashboard > Connect la cadena de conexión adecuada para el entorno de
Render y guardarla en Render como `DATABASE_URL`. Usar el pooler recomendado
por Supabase si el proveedor limita las conexiones directas. La cadena debe
incluir usuario, contraseña, host, puerto, base y `sslmode=require`.

En este servicio no es necesario configurar `SUPABASE_URL`,
`SUPABASE_PUBLISHABLE_KEY`, `SUPABASE_SECRET_KEY` ni `SUPABASE_JWKS_URL`:
`metrics` no llama a la API de Supabase ni valida tokens de Supabase. La
identidad continúa siendo validada por Kong y la base se usa como PostgreSQL
de la proyección de métricas.

Para el workflow de pull requests no se deben agregar secretos de Supabase.
El CI seguirá usando PostgreSQL efímero mediante Docker Compose, de modo que
los PR no dependan de una base de producción.

## Desarrollo local

Requisitos:

- Go 1.22 o compatible;
- Docker Desktop;
- Docker Compose.

```bash
cp .env.example .env
docker compose up --build
```

La API queda disponible en `http://localhost:8080`, PostgreSQL en
`localhost:5432`, RabbitMQ en `localhost:5672` y la consola de RabbitMQ en
`http://localhost:15672`.

Para ejecutar solamente el binario contra dependencias ya levantadas:

```bash
make run
```

## Estructura

```text
metrics/
├── cmd/api/
│   └── main.go                 configuración, router y ciclo de vida
├── internal/
│   ├── events/                 consumidor RabbitMQ e idempotencia de eventos
│   ├── handler/                adaptadores HTTP y códigos de respuesta
│   ├── middleware/             identidad derivada del JWT/header local
│   ├── model/                  DTOs de métricas e incidentes
│   ├── repository/             consultas y persistencia PostgreSQL
│   └── service/                períodos, autorización y exportación CSV
├── migrations/                 migraciones de PostgreSQL
├── .github/workflows/ci.yml    formato, tests, vet, build e imagen Docker
├── Dockerfile                  imagen multi-stage de producción
├── docker-compose.yml          API, PostgreSQL, migraciones y RabbitMQ
├── Makefile                    comandos de desarrollo
├── openapi.yaml                contrato HTTP
└── .env.example                configuración sin secretos
```

## Integración con el stack

El compose de [infrastructure](../infrastructure/) agrega:

- `metrics-db`;
- `metrics-migrations`;
- `metrics`;
- el volumen `metrics_db_data`;
- la configuración necesaria para RabbitMQ y `servers`.

Kong expone las rutas mediante `metrics-service` y aplica JWT. En un despliegue
distribuido, configurar `METRICS_UPSTREAM_URL` en
[gateway/.env.example](../gateway/.env.example) con la URL interna o de
Tailscale correspondiente.

## Validación

Desde este módulo:

```bash
gofmt -l .
go vet ./...
go test ./... -cover
go build ./...
docker build -t discordia-metrics:ci .
```

El workflow de GitHub Actions ejecuta automáticamente `gofmt`, `go build`,
`go vet`, `go test` y el build de la imagen Docker en cada push y pull request.
